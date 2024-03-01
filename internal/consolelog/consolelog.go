// Copyright 2024 Blink Labs Software
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package consolelog

import (
	"context"
	"fmt"
	"io"
	"log/slog"
)

const (
	colorBrightRed     = "91"
	colorBrightYellow  = "93"
	colorBrightMagenta = "95"
)

type Handler struct {
	h   slog.Handler
	out io.Writer
}

func NewHandler(out io.Writer, opts *slog.HandlerOptions) *Handler {
	if opts == nil {
		opts = &slog.HandlerOptions{}
	}
	return &Handler{
		out: out,
		h: slog.NewTextHandler(out, &slog.HandlerOptions{
			Level: opts.Level,
		}),
	}
}

func (h *Handler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.h.Enabled(ctx, level)
}

func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &Handler{h: h.h.WithAttrs(attrs)}
}

func (h *Handler) WithGroup(name string) slog.Handler {
	return &Handler{h: h.h.WithGroup(name)}
}

func (h *Handler) Handle(ctx context.Context, r slog.Record) error {
	var levelTag string
	switch r.Level {
	case slog.LevelDebug:
		levelTag = fmt.Sprintf("\033[%smDEBUG:\033[0m ", colorBrightMagenta)
	case slog.LevelInfo:
		// No tag for INFO
		levelTag = ""
	case slog.LevelWarn:
		levelTag = fmt.Sprintf("\033[%smWARNING:\033[0m ", colorBrightYellow)
	case slog.LevelError:
		levelTag = fmt.Sprintf("\033[%smERROR:\033[0m ", colorBrightRed)
	}
	msg := levelTag + r.Message + "\n"
	if _, err := h.out.Write([]byte(msg)); err != nil {
		return err
	}
	return nil
}
