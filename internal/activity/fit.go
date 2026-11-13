package activity

import (
	"fmt"
	"io"
	"math"
	"sort"
	"time"

	"github.com/muktihari/fit/decoder"
	"github.com/muktihari/fit/kit/datetime"
	"github.com/muktihari/fit/profile/basetype"
	"github.com/muktihari/fit/profile/mesgdef"
	"github.com/muktihari/fit/profile/typedef"
	"github.com/muktihari/fit/proto"
)

type gearEvent struct {
	timestamp time.Time
	front     Value
	rear      Value
}

type developerFieldKey struct {
	index  uint8
	number uint8
}

type developerFieldDescription struct {
	name     string
	baseType basetype.BaseType
	scale    uint8
	offset   int8
}

func loadFIT(r io.Reader, customFieldIndices map[string]int) (*Activity, error) {
	decoded, err := decoder.New(r).Decode()
	if err != nil {
		return nil, fmt.Errorf("decode FIT: %w", err)
	}
	developerFields, err := fitDeveloperFieldDescriptions(decoded.Messages, customFieldIndices)
	if err != nil {
		return nil, err
	}
	samples := make([]Sample, 0, len(decoded.Messages)/4)
	gearEvents := make([]gearEvent, 0)
	activityCustomFields := make([]Value, len(customFieldIndices))
	for i := range decoded.Messages {
		message := &decoded.Messages[i]
		switch message.Num.String() {
		case "record":
			sample, ok := parseFITRecord(message, customFieldIndices, developerFields)
			if ok {
				samples = append(samples, sample)
			}
		case "session":
			values := fitCustomFieldValues(message, customFieldIndices, developerFields)
			for i, v := range values {
				if v.Valid {
					activityCustomFields[i] = v
				}
			}
		case "event":
			event, ok := parseFITGearEvent(message)
			if ok {
				gearEvents = append(gearEvents, event)
			}
		}
	}
	if len(samples) == 0 {
		return nil, fmt.Errorf("FIT file contains no GPS record samples")
	}
	sort.SliceStable(gearEvents, func(i, j int) bool { return gearEvents[i].timestamp.Before(gearEvents[j].timestamp) })
	sort.SliceStable(samples, func(i, j int) bool { return samples[i].Timestamp.Before(samples[j].Timestamp) })
	applyGearEvents(samples, gearEvents)
	a, err := orderedActivity(samples)
	if err != nil {
		return nil, err
	}
	a.CustomFieldIndices = customFieldIndices
	a.CustomFieldValues = activityCustomFields
	return a, nil
}

func fitDeveloperFieldDescriptions(messages []proto.Message, indices map[string]int) (map[developerFieldKey]developerFieldDescription, error) {
	if len(indices) == 0 {
		return nil, nil
	}
	descriptions := make(map[developerFieldKey]developerFieldDescription)
	names := make(map[string]developerFieldKey)
	for i := range messages {
		if messages[i].Num.String() != "field_description" {
			continue
		}
		field := mesgdef.NewFieldDescription(&messages[i])
		if len(field.FieldName) != 1 || field.FieldName[0] == "" {
			continue
		}
		name := field.FieldName[0]
		if _, ok := indices[name]; !ok {
			continue
		}
		key := developerFieldKey{index: field.DeveloperDataIndex, number: field.FieldDefinitionNumber}
		if previous, ok := names[name]; ok && previous != key {
			return nil, fmt.Errorf("custom FIT field %q is ambiguous: multiple Developer Data fields use that name", name)
		}
		names[name] = key
		descriptions[key] = developerFieldDescription{
			name: name, baseType: field.FitBaseTypeId, scale: field.Scale, offset: field.Offset,
		}
	}
	return descriptions, nil
}

func fitCustomFieldValues(message *proto.Message, indices map[string]int, descriptions map[developerFieldKey]developerFieldDescription) []Value {
	if len(indices) == 0 {
		return nil
	}
	values := make([]Value, len(indices))
	for _, field := range message.Fields {
		index, requested := indices[field.Name]
		if !requested {
			continue
		}
		if number, ok := fitFieldNumber(&field); ok {
			values[index] = value(number)
		}
	}
	for _, field := range message.DeveloperFields {
		description, ok := descriptions[developerFieldKey{index: field.DeveloperDataIndex, number: field.Num}]
		if !ok {
			continue
		}
		index := indices[description.name]
		base := &proto.FieldBase{BaseType: description.baseType}
		if description.scale != uint8(basetype.Uint8Invalid) {
			base.Scale = float64(description.scale)
		}
		if description.offset != int8(basetype.Sint8Invalid) {
			base.Offset = float64(description.offset)
		}
		if number, ok := fitFieldNumber(&proto.Field{FieldBase: base, Value: field.Value}); ok {
			values[index] = value(number)
		}
	}
	return values
}

func parseFITRecord(message *proto.Message, customFieldIndices map[string]int, developerFields map[developerFieldKey]developerFieldDescription) (Sample, bool) {
	timestamp, ok := fitTime(message, "timestamp")
	if !ok {
		return Sample{}, false
	}
	lat, latOK := fitNumber(message, "position_lat")
	lon, lonOK := fitNumber(message, "position_long")
	if !latOK || !lonOK {
		return Sample{}, false
	}
	sample := Sample{
		Timestamp:         timestamp,
		Latitude:          value(lat * 180 / float64(1<<31)),
		Longitude:         value(lon * 180 / float64(1<<31)),
		GPSFix:            value(3),
		CustomFieldValues: fitCustomFieldValues(message, customFieldIndices, developerFields),
	}
	for _, field := range message.Fields {
		number, ok := fitFieldNumber(&field)
		if !ok {
			continue
		}
		var dst *Value
		switch field.Name {
		case "distance":
			dst = &sample.Distance
		case "altitude", "enhanced_altitude":
			dst = &sample.Altitude
		case "speed", "enhanced_speed":
			dst = &sample.Speed
		case "vertical_speed":
			dst = &sample.VerticalSpeed
		case "heart_rate":
			dst = &sample.HR
		case "cadence":
			dst = &sample.Cadence
		case "power":
			dst = &sample.Power
		case "temperature":
			dst = &sample.Temperature
		case "grade":
			dst = &sample.Grade
		case "gps_accuracy":
			dst = &sample.GPSAccuracy
		case "front_gear_num":
			dst = &sample.FrontGear
		case "rear_gear_num":
			dst = &sample.RearGear
		case "enhanced_respiration_rate", "unknown_108":
			dst = &sample.Respiration
		case "Sdps", "sdps":
			dst = &sample.SDPS
		}
		if dst != nil {
			setValue(dst, number)
		}
	}
	return sample, true
}

func parseFITGearEvent(message *proto.Message) (gearEvent, bool) {
	eventField := fitFieldByName(message, "event")
	if eventField == nil || eventField.Value.Type() == proto.TypeInvalid {
		return gearEvent{}, false
	}
	eventType := typedef.Event(eventField.Value.Uint8())
	if eventType != typedef.EventFrontGearChange && eventType != typedef.EventRearGearChange {
		return gearEvent{}, false
	}
	timestamp, ok := fitTime(message, "timestamp")
	if !ok {
		return gearEvent{}, false
	}
	event := gearEvent{timestamp: timestamp}
	if number, ok := fitNumber(message, "front_gear_num"); ok {
		event.front = value(number)
	}
	if number, ok := fitNumber(message, "rear_gear_num"); ok {
		event.rear = value(number)
	}
	return event, event.front.Valid || event.rear.Valid
}

func applyGearEvents(samples []Sample, events []gearEvent) {
	front, rear := Value{}, Value{}
	eventIndex := 0
	for i := range samples {
		for eventIndex < len(events) && !events[eventIndex].timestamp.After(samples[i].Timestamp) {
			if events[eventIndex].front.Valid {
				front = events[eventIndex].front
			}
			if events[eventIndex].rear.Valid {
				rear = events[eventIndex].rear
			}
			eventIndex++
		}
		samples[i].FrontGear = front
		samples[i].RearGear = rear
	}
}

func fitTime(message *proto.Message, name string) (time.Time, bool) {
	field := fitFieldByName(message, name)
	if field == nil || fitInvalid(field) {
		return time.Time{}, false
	}
	return datetime.ToTime(field.Value.Uint32()), true
}

func fitNumber(message *proto.Message, name string) (float64, bool) {
	field := fitFieldByName(message, name)
	if field == nil {
		return 0, false
	}
	return fitFieldNumber(field)
}

func fitFieldByName(message *proto.Message, name string) *proto.Field {
	for i := range message.Fields {
		if message.Fields[i].Name == name {
			return &message.Fields[i]
		}
	}
	return nil
}

func fitFieldNumber(field *proto.Field) (float64, bool) {
	if fitInvalid(field) {
		return 0, false
	}
	number, ok := fitRawNumber(field.Value)
	if !ok {
		return 0, false
	}
	if field.Scale > 0 {
		number /= field.Scale
	}
	number -= field.Offset
	if math.IsNaN(number) || math.IsInf(number, 0) {
		return 0, false
	}
	return number, true
}

func fitRawNumber(value proto.Value) (float64, bool) {
	switch number := value.Any().(type) {
	case int8:
		return float64(number), true
	case uint8:
		return float64(number), true
	case int16:
		return float64(number), true
	case uint16:
		return float64(number), true
	case int32:
		return float64(number), true
	case uint32:
		return float64(number), true
	case int64:
		return float64(number), true
	case uint64:
		return float64(number), true
	case float32:
		return float64(number), true
	case float64:
		return number, true
	default:
		return 0, false
	}
}

func fitInvalid(field *proto.Field) bool {
	if field.Value.Type() == proto.TypeInvalid {
		return true
	}
	raw, ok := fitRawNumber(field.Value)
	if !ok {
		return true
	}
	switch field.BaseType {
	case basetype.Enum, basetype.Uint8:
		return raw == float64(basetype.Uint8Invalid)
	case basetype.Uint8z:
		return raw == float64(basetype.Uint8zInvalid)
	case basetype.Sint8:
		return raw == float64(basetype.Sint8Invalid)
	case basetype.Uint16:
		return raw == float64(basetype.Uint16Invalid)
	case basetype.Uint16z:
		return raw == float64(basetype.Uint16zInvalid)
	case basetype.Sint16:
		return raw == float64(basetype.Sint16Invalid)
	case basetype.Uint32:
		return raw == float64(basetype.Uint32Invalid)
	case basetype.Uint32z:
		return raw == float64(basetype.Uint32zInvalid)
	case basetype.Sint32:
		return raw == float64(basetype.Sint32Invalid)
	case basetype.Uint64:
		return raw == float64(basetype.Uint64Invalid)
	case basetype.Uint64z:
		return raw == float64(basetype.Uint64zInvalid)
	case basetype.Sint64:
		return raw == float64(basetype.Sint64Invalid)
	case basetype.Float32, basetype.Float64:
		return math.IsNaN(raw)
	default:
		return false
	}
}
