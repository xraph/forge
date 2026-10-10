package workbench

import (
	"bytes"
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/xraph/forge/cmd/forge/internal/deploy/engine"
	"github.com/xraph/forge/cmd/forge/internal/deploy/provider"
	"gopkg.in/yaml.v3"
)

// publicSettings excludes runtime and build configuration from the editor.
func publicSettings(view engine.SettingsView) engine.SettingsView {
	view.Files = append([]engine.FileView(nil), view.Files...)
	for i, file := range view.Files {
		if file.Path != ".forge.yml" && file.Path != ".forge.yaml" {
			continue
		}

		var node yaml.Node

		view.Files[i].Content = ""
		if yaml.Unmarshal([]byte(file.Content), &node) != nil || len(node.Content) == 0 {
			continue
		}

		root := node.Content[0]
		for n := 0; n+1 < len(root.Content); n += 2 {
			if root.Content[n].Value != "deploy" {
				continue
			}

			mapping := yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{root.Content[n], root.Content[n+1]}}

			var buffer bytes.Buffer

			encoder := yaml.NewEncoder(&buffer)
			encoder.SetIndent(2)

			if encoder.Encode(&mapping) == nil {
				view.Files[i].Content = buffer.String()
			}

			_ = encoder.Close()

			break
		}
	}

	return view
}
func (s *Server) syncMutation(fn func() error) error {
	if !s.mutations.TryLock() {
		return errBusy
	}
	defer s.mutations.Unlock()

	s.operations.Lock()
	busy := s.active != nil
	s.operations.Unlock()

	select {
	case <-s.done:
		return errBusy
	default:
	}

	if busy {
		return errBusy
	}

	return fn()
}
func (s *Server) startRun(action, target, env string, fn func(context.Context, chan<- provider.Event) error) (Run, error) {
	if !s.mutations.TryLock() {
		return Run{}, errBusy
	}
	defer s.mutations.Unlock()

	return s.beginRun(action, target, env, fn)
}
func (s *Server) logs(w http.ResponseWriter, r *http.Request, ctx context.Context) {
	query := r.URL.Query()
	tail := 200

	if value := query.Get("tail"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 0 || parsed > 10000 {
			apiFailure(w, 400, "tail must be between 0 and 10000", nil)

			return
		}

		tail = parsed
	}

	follow := query.Get("follow") == "true"

	reader, err := s.engine.Logs(ctx, query.Get("target"), query.Get("env"), query.Get("service"), provider.LogOptions{Tail: tail, Follow: follow})
	if err != nil {
		failure(w, err)

		return
	}
	defer reader.Close()

	stop := context.AfterFunc(ctx, func() { _ = reader.Close() })
	defer stop()

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	buffer := make([]byte, 32*1024)
	limit := int64(4 << 20)

	for {
		n, readErr := reader.Read(buffer)
		if n > 0 {
			if !follow && int64(n) > limit {
				n = int(limit)
			}

			_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(15 * time.Second))
			if _, err := w.Write(buffer[:n]); err != nil {
				return
			}

			if flush, ok := w.(http.Flusher); ok {
				flush.Flush()
			}

			limit -= int64(n)
		}

		if readErr != nil || (!follow && limit <= 0) || ctx.Err() != nil {
			return
		}
	}
}
