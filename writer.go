package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
)

// localWriter serves as a controlled process' stdout and stderr.
// It unifies the output format.
type localWriter struct {
	buf      []byte
	isStderr bool
	logger   *slog.Logger
}

func (w *localWriter) Write(p []byte) (int, error) {
	// Assume anything that's in stderr is an error and not structured.
	if w.isStderr {
		w.logger.Error(strings.Trim(string(p), "\n"))
		return len(p), nil
	}

	w.buf = append(w.buf, p...)
	for {
		pos := bytes.IndexByte(w.buf, '\n')
		if pos < 0 {
			break
		}
		var m map[string]any
		err := json.Unmarshal(w.buf[0:pos], &m)
		if err == nil {
			w.pass(m)
		} else {
			w.logger.Warn(string(w.buf[0:pos]))
		}
		rest := w.buf[pos+1:]
		ll := len(rest)
		copy(w.buf, rest)
		w.buf = w.buf[0:ll]
	}
	return len(p), nil
}

func (w *localWriter) pass(m map[string]any) {
	level := slog.LevelInfo
	msg := ""
	var attrs []slog.Attr
	for k, v := range m {
		if k == "msg" {
			m, ok := v.(string)
			if ok {
				msg = m
			} else {
				attrs = append(attrs, slog.Any("orig_msg", v))
			}
			continue
		}
		if k == "level" {
			switch v {
			case "info":
				level = slog.LevelInfo
			case "warn":
				level = slog.LevelWarn
			case "error":
				level = slog.LevelError
			default:
				level = slog.LevelWarn
				attrs = append(attrs, slog.Any("orig_level", v))
			}
			continue
		}
		attrs = append(attrs, slog.Any(k, v))
	}
	w.logger.LogAttrs(context.Background(), level, msg, attrs...)
}
