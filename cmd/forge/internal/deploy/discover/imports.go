package discover

import (
	"context"
	"encoding/json"
	"slices"
	"strings"

	"github.com/xraph/forge/cmd/forge/internal/deploy/catalog"
	"github.com/xraph/forge/cmd/forge/internal/deploy/execx"
	"github.com/xraph/forge/cmd/forge/internal/deploy/model"
)

var importKinds = map[string]model.ResourceType{
	"github.com/xraph/grove/drivers/pgdriver":           model.Postgres,
	"github.com/xraph/grove/drivers/mysqldriver":        model.MySQL,
	"github.com/xraph/grove/drivers/sqlitedriver":       model.SQLite,
	"github.com/xraph/grove/drivers/mongodriver":        model.MongoDB,
	"github.com/xraph/grove/drivers/clickhousedriver":   model.ClickHouse,
	"github.com/xraph/grove/drivers/tursodriver":        model.Turso,
	"github.com/xraph/grove/kv/drivers/redisdriver":     model.Redis,
	"github.com/xraph/grove/kv/drivers/memcacheddriver": model.Memcached,
	"github.com/xraph/trove/drivers/s3driver":           model.ObjectStorage,
	"github.com/xraph/trove/drivers/gcsdriver":          model.ObjectStorage,
	"github.com/nats-io/nats.go":                        model.NATS,
	"github.com/IBM/sarama":                             model.Kafka,
	"github.com/rabbitmq/amqp091-go":                    model.RabbitMQ,
	"github.com/redis/go-redis/v9":                      model.Redis,
}

func goList(ctx context.Context, r execx.Runner, root string, args ...string) (string, error) {
	res, err := r.Run(ctx, execx.Command{Name: "go", Args: append([]string{"list"}, args...), Dir: root, Env: []string{"GOWORK=off"}})
	if err != nil {
		return "", err
	}

	return res.Stdout, nil
}

func modules(ctx context.Context, r execx.Runner, root string) []catalog.Module {
	out, err := goList(ctx, r, root, "-m", "-json", "all")
	if err != nil {
		return nil
	}

	var mods []catalog.Module

	dec := json.NewDecoder(strings.NewReader(out))
	for dec.More() {
		var m struct {
			Path string `json:"Path"`
			Dir  string `json:"Dir"`
		}
		if err := dec.Decode(&m); err != nil {
			break
		}

		if m.Dir != "" {
			mods = append(mods, catalog.Module{Path: m.Path, Dir: m.Dir})
		}
	}

	return mods
}

func imports(ctx context.Context, r execx.Runner, root, mainPath string) ([]string, bool) {
	out, err := goList(ctx, r, root, "-deps", "-f", "{{.ImportPath}}", "./"+mainPath)
	if err != nil {
		return nil, false
	}

	seen := map[string]bool{}

	var list []string

	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		if line != "" && !seen[line] {
			seen[line] = true
			list = append(list, line)
		}
	}

	return list, true
}

func hasHTTP(list []string) bool {
	return slices.Contains(list, "net/http")
}
