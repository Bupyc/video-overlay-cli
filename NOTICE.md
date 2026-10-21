# Notices

English | [Русский](NOTICE.ru.md)

This project is a Go reimplementation of selected functionality from [gopro-dashboard-overlay](https://github.com/time4tea/gopro-dashboard-overlay), licensed under GPL-3.0-or-later. Directly ported code and bundled assets will retain their upstream copyright and license notices.

The XML layouts in `examples/reference-layouts/` are based on files from that project's `gopro_overlay/layouts` and `examples/layout` directories and remain under GPL-3.0-or-later.

## Ramer-Douglas-Peucker route simplification

Adapted from `gopro_overlay/rdp.py`, Copyright (c) 2019 Sean Bleier, MIT License:

Permission is hereby granted, free of charge, to any person obtaining a copy of this software and associated documentation files (the "Software"), to deal in the Software without restriction, including without limitation the rights to use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of the Software, and to permit persons to whom the Software is furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.

## Web Mercator tile geometry

The projection and tile behavior are based on the GeoTiler implementation in the reference project, Copyright (C) 2014-2023 Artur Wroblewski, licensed under GPL-3.0-or-later. Its source also identifies the ModestMaps projection work, Copyright (C) 2007-2013 Michal Migurski and contributors, under a BSD license.
