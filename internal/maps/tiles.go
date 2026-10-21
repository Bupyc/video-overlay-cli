package maps

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/draw"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const defaultTileURL = "https://tile.openstreetmap.org/{z}/{x}/{y}.png"

type Config struct {
	TileURL     string
	Attribution string
	UserAgent   string
	Client      *http.Client
	Cache       *Cache
}

type Request struct {
	Viewport Viewport
	Config   Config
}

type Cache struct {
	dir      string
	mu       sync.Mutex
	items    map[string]cacheEntry
	inflight map[string]*tileResult
	limit    chan struct{}
}

type cacheEntry struct {
	data         []byte
	img          image.Image
	expires      time.Time
	etag         string
	lastModified string
}

type diskMetadata struct {
	Expires      time.Time `json:"expires"`
	ETag         string    `json:"etag,omitempty"`
	LastModified string    `json:"last_modified,omitempty"`
}

type tileResult struct {
	img  image.Image
	err  error
	done chan struct{}
}

type tileJob struct {
	x, y int64
	url  string
}

type renderedTile struct {
	job tileJob
	img image.Image
	err error
}

func DefaultConfig() Config {
	return Config{
		TileURL:     defaultTileURL,
		Attribution: "© OpenStreetMap contributors",
		UserAgent:   "video-overlay-cli/0.1",
		Client:      &http.Client{Timeout: 15 * time.Second},
		Cache:       NewCache(""),
	}
}

func (c Config) WithDefaults() Config {
	if c.TileURL == "" {
		c.TileURL = defaultTileURL
	}
	if c.Attribution == "" {
		c.Attribution = "© OpenStreetMap contributors"
	}
	if c.UserAgent == "" {
		c.UserAgent = "video-overlay-cli/0.1"
	}
	if c.Client == nil {
		c.Client = &http.Client{Timeout: 15 * time.Second}
	}
	if c.Cache == nil {
		c.Cache = NewCache("")
	}
	return c
}

func NewCache(dir string) *Cache {
	return &Cache{dir: dir, items: make(map[string]cacheEntry), inflight: make(map[string]*tileResult), limit: make(chan struct{}, 2)}
}

func Render(ctx context.Context, request Request) (*image.RGBA, error) {
	return render(ctx, request, nil)
}

func RenderInto(ctx context.Context, request Request, result *image.RGBA) error {
	if result == nil {
		return fmt.Errorf("map render destination is nil")
	}
	_, err := render(ctx, request, result)
	return err
}

func render(ctx context.Context, request Request, result *image.RGBA) (*image.RGBA, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	config := request.Config.WithDefaults()
	viewport, err := NewViewport(request.Viewport.Center, request.Viewport.Zoom, request.Viewport.Size)
	if err != nil {
		return nil, err
	}
	if !strings.Contains(config.TileURL, "{z}") || !strings.Contains(config.TileURL, "{x}") || !strings.Contains(config.TileURL, "{y}") {
		return nil, fmt.Errorf("tile URL must contain {z}, {x}, and {y} placeholders")
	}
	bounds := image.Rect(0, 0, viewport.Size.X, viewport.Size.Y)
	if result == nil {
		result = image.NewRGBA(bounds)
	} else {
		if result.Bounds() != bounds || result.Stride != bounds.Dx()*4 || len(result.Pix) != bounds.Dy()*result.Stride {
			return nil, fmt.Errorf("map render destination must be a tightly packed %dx%d RGBA image", bounds.Dx(), bounds.Dy())
		}
		clear(result.Pix)
	}
	centerX, centerY := ProjectPixel(viewport.Center, viewport.Zoom)
	left := centerX - float64(viewport.Size.X)/2
	top := centerY - float64(viewport.Size.Y)/2
	firstX := int64(floor(left / TileSize))
	lastX := int64(floor((left + float64(viewport.Size.X) - 1) / TileSize))
	firstY := int64(floor(top / TileSize))
	lastY := int64(floor((top + float64(viewport.Size.Y) - 1) / TileSize))
	worldTiles := int64(1) << viewport.Zoom
	jobs := make([]tileJob, 0, int((lastX-firstX+1)*(lastY-firstY+1)))
	for y := firstY; y <= lastY; y++ {
		if y < 0 || y >= worldTiles {
			continue
		}
		for x := firstX; x <= lastX; x++ {
			normalizedX := x % worldTiles
			if normalizedX < 0 {
				normalizedX += worldTiles
			}
			url := strings.NewReplacer(
				"{z}", strconv.Itoa(viewport.Zoom),
				"{x}", strconv.FormatInt(normalizedX, 10),
				"{y}", strconv.FormatInt(y, 10),
				"{ext}", "png",
			).Replace(config.TileURL)
			jobs = append(jobs, tileJob{x: x, y: y, url: url})
		}
	}
	if len(jobs) == 0 {
		return result, nil
	}
	pendingJobs := make([]tileJob, 0, len(jobs))
	for _, job := range jobs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if tile, ok := config.Cache.cached(job.url); ok {
			if err := placeTile(result, job, tile, left, top, worldTiles, viewport.Zoom); err != nil {
				return nil, err
			}
		} else {
			pendingJobs = append(pendingJobs, job)
		}
	}
	if len(pendingJobs) == 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return result, nil
	}
	workerCount := min(2, len(pendingJobs))
	jobChannel := make(chan tileJob, len(pendingJobs))
	resultChannel := make(chan renderedTile, workerCount)
	for _, job := range pendingJobs {
		jobChannel <- job
	}
	close(jobChannel)
	mapCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var workers sync.WaitGroup
	workers.Add(workerCount)
	for range workerCount {
		go func() {
			defer workers.Done()
			for job := range jobChannel {
				tile, err := config.Cache.get(mapCtx, job.url, config.UserAgent, config.Client)
				resultChannel <- renderedTile{job: job, img: tile, err: err}
			}
		}()
	}
	go func() {
		workers.Wait()
		close(resultChannel)
	}()
	var firstErr error
	for tileResult := range resultChannel {
		if tileResult.err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("load map tile %d/%d/%d: %w", viewport.Zoom, wrapTileX(tileResult.job.x, worldTiles), tileResult.job.y, tileResult.err)
				cancel()
			}
			continue
		}
		if firstErr != nil {
			continue
		}
		if err := placeTile(result, tileResult.job, tileResult.img, left, top, worldTiles, viewport.Zoom); err != nil {
			firstErr = err
			cancel()
			continue
		}
	}
	if firstErr != nil {
		return nil, firstErr
	}
	return result, nil
}

func placeTile(result *image.RGBA, job tileJob, tile image.Image, left, top float64, worldTiles int64, zoom int) error {
	if tile.Bounds().Dx() != TileSize || tile.Bounds().Dy() != TileSize {
		return fmt.Errorf("map tile %d/%d/%d has size %dx%d, want %dx%d", zoom, wrapTileX(job.x, worldTiles), job.y, tile.Bounds().Dx(), tile.Bounds().Dy(), TileSize, TileSize)
	}
	dx := int(job.x*TileSize - int64(floor(left)))
	dy := int(job.y*TileSize - int64(floor(top)))
	destination := image.Rect(dx, dy, dx+TileSize, dy+TileSize).Intersect(result.Bounds())
	source := image.Pt(destination.Min.X-dx, destination.Min.Y-dy)
	draw.Draw(result, destination, tile, source, draw.Src)
	return nil
}

func wrapTileX(x, worldTiles int64) int64 {
	x %= worldTiles
	if x < 0 {
		x += worldTiles
	}
	return x
}

func (c *Cache) cached(url string) (image.Image, bool) {
	c.mu.Lock()
	entry, ok := c.items[url]
	c.mu.Unlock()
	if !ok || !time.Now().Before(entry.expires) {
		return nil, false
	}
	return entry.img, true
}

func (c *Cache) get(ctx context.Context, url, userAgent string, client *http.Client) (image.Image, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	if entry, ok := c.items[url]; ok && time.Now().Before(entry.expires) {
		c.mu.Unlock()
		return entry.img, nil
	}
	if pending := c.inflight[url]; pending != nil {
		c.mu.Unlock()
		select {
		case <-pending.done:
			return pending.img, pending.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	pending := &tileResult{done: make(chan struct{})}
	c.inflight[url] = pending
	stale, ok := c.items[url]
	c.mu.Unlock()

	select {
	case c.limit <- struct{}{}:
	case <-ctx.Done():
		pending.err = ctx.Err()
		c.finish(url, pending, cacheEntry{}, false)
		return nil, ctx.Err()
	}
	defer func() { <-c.limit }()
	if !ok {
		stale, ok, pending.err = c.read(url)
	}
	if pending.err == nil && ok && time.Now().Before(stale.expires) {
		c.finish(url, pending, stale, true)
		return stale.img, nil
	}
	if pending.err == nil {
		var entry cacheEntry
		var keep bool
		entry, keep, pending.err = c.fetch(ctx, url, userAgent, client, stale, ok)
		if pending.err == nil {
			pending.img = entry.img
			c.finish(url, pending, entry, keep)
			return entry.img, nil
		}
	}
	c.finish(url, pending, cacheEntry{}, false)
	return nil, pending.err
}

func (c *Cache) finish(url string, pending *tileResult, entry cacheEntry, keep bool) {
	c.mu.Lock()
	if keep {
		if len(c.items) >= 512 {
			for key := range c.items {
				delete(c.items, key)
				break
			}
		}
		c.items[url] = entry
	} else {
		delete(c.items, url)
	}
	pending.img = entry.img
	delete(c.inflight, url)
	close(pending.done)
	c.mu.Unlock()
}

func (c *Cache) fetch(ctx context.Context, url, userAgent string, client *http.Client, stale cacheEntry, haveStale bool) (cacheEntry, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return cacheEntry{}, false, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "image/png,image/jpeg,*/*;q=0.8")
	if haveStale {
		if stale.etag != "" {
			req.Header.Set("If-None-Match", stale.etag)
		}
		if stale.lastModified != "" {
			req.Header.Set("If-Modified-Since", stale.lastModified)
		}
	}
	response, err := client.Do(req)
	if err != nil {
		return cacheEntry{}, false, err
	}
	defer response.Body.Close()
	now := time.Now()
	if response.StatusCode == http.StatusNotModified && haveStale {
		stale.expires = expiry(response.Header, now)
		stale.etag = firstNonEmpty(response.Header.Get("ETag"), stale.etag)
		stale.lastModified = firstNonEmpty(response.Header.Get("Last-Modified"), stale.lastModified)
		if !strings.Contains(strings.ToLower(response.Header.Get("Cache-Control")), "no-store") {
			if err := c.write(url, stale); err != nil {
				return cacheEntry{}, false, err
			}
		} else {
			stale.expires = now
			if err := c.remove(url); err != nil {
				return cacheEntry{}, false, err
			}
		}
		return stale, stale.expires.After(now), nil
	}
	if response.StatusCode != http.StatusOK {
		return cacheEntry{}, false, fmt.Errorf("tile server returned HTTP %s", response.Status)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, (10<<20)+1))
	if err != nil {
		return cacheEntry{}, false, fmt.Errorf("read tile response: %w", err)
	}
	if len(data) > 10<<20 {
		return cacheEntry{}, false, fmt.Errorf("tile response exceeds 10 MiB")
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return cacheEntry{}, false, fmt.Errorf("decode tile image: %w", err)
	}
	if img.Bounds().Dx() != TileSize || img.Bounds().Dy() != TileSize {
		return cacheEntry{}, false, fmt.Errorf("tile image has size %dx%d, want %dx%d", img.Bounds().Dx(), img.Bounds().Dy(), TileSize, TileSize)
	}
	entry := cacheEntry{
		data: data, img: img, expires: expiry(response.Header, now),
		etag: response.Header.Get("ETag"), lastModified: response.Header.Get("Last-Modified"),
	}
	if !strings.Contains(strings.ToLower(response.Header.Get("Cache-Control")), "no-store") {
		if err := c.write(url, entry); err != nil {
			return cacheEntry{}, false, err
		}
	} else {
		entry.expires = now
		if err := c.remove(url); err != nil {
			return cacheEntry{}, false, err
		}
	}
	return entry, entry.expires.After(now), nil
}

func (c *Cache) read(url string) (cacheEntry, bool, error) {
	if c.dir == "" {
		return cacheEntry{}, false, nil
	}
	dataPath, metadataPath := c.paths(url)
	data, err := os.ReadFile(dataPath)
	if err != nil {
		if os.IsNotExist(err) {
			return cacheEntry{}, false, nil
		}
		return cacheEntry{}, false, err
	}
	metadataBytes, err := os.ReadFile(metadataPath)
	if err != nil {
		if os.IsNotExist(err) {
			return cacheEntry{}, false, nil
		}
		return cacheEntry{}, false, err
	}
	var metadata diskMetadata
	if err := json.Unmarshal(metadataBytes, &metadata); err != nil {
		return cacheEntry{}, false, fmt.Errorf("read tile cache metadata: %w", err)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return cacheEntry{}, false, fmt.Errorf("decode cached tile: %w", err)
	}
	if img.Bounds().Dx() != TileSize || img.Bounds().Dy() != TileSize {
		return cacheEntry{}, false, fmt.Errorf("cached tile has size %dx%d, want %dx%d", img.Bounds().Dx(), img.Bounds().Dy(), TileSize, TileSize)
	}
	return cacheEntry{data: data, img: img, expires: metadata.Expires, etag: metadata.ETag, lastModified: metadata.LastModified}, true, nil
}

func (c *Cache) write(url string, entry cacheEntry) error {
	if c.dir == "" {
		return nil
	}
	dataPath, metadataPath := c.paths(url)
	if err := os.MkdirAll(c.dir, 0o755); err != nil {
		return fmt.Errorf("create tile cache directory: %w", err)
	}
	metadata, err := json.Marshal(diskMetadata{Expires: entry.expires, ETag: entry.etag, LastModified: entry.lastModified})
	if err != nil {
		return err
	}
	if err := writeAtomic(dataPath, entry.data); err != nil {
		return fmt.Errorf("write tile cache: %w", err)
	}
	if err := writeAtomic(metadataPath, metadata); err != nil {
		return fmt.Errorf("write tile cache metadata: %w", err)
	}
	return nil
}

func (c *Cache) paths(url string) (string, string) {
	hash := sha256.Sum256([]byte(url))
	name := hex.EncodeToString(hash[:])
	return filepath.Join(c.dir, name+".tile"), filepath.Join(c.dir, name+".json")
}

func (c *Cache) remove(url string) error {
	if c.dir == "" {
		return nil
	}
	dataPath, metadataPath := c.paths(url)
	for _, path := range []string{dataPath, metadataPath} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func writeAtomic(path string, data []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".tile-*")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

func expiry(header http.Header, now time.Time) time.Time {
	for _, directive := range strings.Split(header.Get("Cache-Control"), ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(directive), "=")
		if strings.EqualFold(key, "no-store") {
			return now
		}
		if strings.EqualFold(key, "no-cache") {
			return now
		}
		if strings.EqualFold(key, "max-age") && ok {
			seconds, err := strconv.Atoi(strings.Trim(value, `"`))
			if err == nil && seconds >= 0 {
				return now.Add(time.Duration(seconds) * time.Second)
			}
		}
	}
	if value := header.Get("Expires"); value != "" {
		if expires, err := http.ParseTime(value); err == nil {
			return expires
		}
	}
	return now.Add(7 * 24 * time.Hour)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func floor(value float64) float64 {
	return math.Floor(value)
}
