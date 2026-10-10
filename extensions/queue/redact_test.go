package queue

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/xraph/forge"
)

// trickyPassword holds every character that used to break or reshape the
// hand-built AMQP URL.
const trickyPassword = "p@ss:w/rd%41?#& x"

// captureLogger records Info lines and their fields; every other method is
// the embedded no-op logger's.
type captureLogger struct {
	forge.Logger

	mu    sync.Mutex
	lines []string
}

func newCaptureLogger() *captureLogger {
	return &captureLogger{Logger: forge.NewNoopLogger()}
}

func (l *captureLogger) Info(msg string, fields ...forge.Field) {
	var b strings.Builder
	b.WriteString(msg)

	for _, f := range fields {
		fmt.Fprintf(&b, " %s=%v", f.Key(), f.Value())
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	l.lines = append(l.lines, b.String())
}

func (l *captureLogger) line(prefix string) (string, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	for _, s := range l.lines {
		if strings.HasPrefix(s, prefix) {
			return s, true
		}
	}

	return "", false
}

func TestRabbitMQ_amqpURLEscapesCredentials(t *testing.T) {
	q, err := NewRabbitMQQueue(Config{
		Hosts:    []string{"broker.internal:5673"},
		Username: "svc@team:ops",
		Password: trickyPassword,
		VHost:    "/prod",
	}, forge.NewNoopLogger(), forge.NewNoOpMetrics())
	if err != nil {
		t.Fatalf("NewRabbitMQQueue() error = %v", err)
	}

	uri, err := amqp.ParseURI(q.amqpURL())
	if err != nil {
		t.Fatalf("built URL does not parse: %v", err)
	}

	if uri.Username != "svc@team:ops" || uri.Password != trickyPassword {
		t.Errorf("credentials = %q / %q, want %q / %q", uri.Username, uri.Password, "svc@team:ops", trickyPassword)
	}

	if uri.Host != "broker.internal" || uri.Port != 5673 || uri.Vhost != "prod" {
		t.Errorf("host/port/vhost = %s/%d/%s, want broker.internal/5673/prod", uri.Host, uri.Port, uri.Vhost)
	}
}

func TestRabbitMQ_amqpURLVHost(t *testing.T) {
	tests := []struct {
		vhost string
		want  string
	}{
		{"", "/"},
		{"/", "/"},
		{"/prod", "prod"},
		{"prod", "prod"},
		{"//slashed", "/slashed"},
		{"100%", "100%"},
	}

	for _, tt := range tests {
		t.Run(tt.vhost, func(t *testing.T) {
			q, err := NewRabbitMQQueue(Config{
				Hosts:    []string{"localhost"},
				Username: "guest",
				Password: "guest",
				VHost:    tt.vhost,
			}, forge.NewNoopLogger(), forge.NewNoOpMetrics())
			if err != nil {
				t.Fatalf("NewRabbitMQQueue() error = %v", err)
			}

			uri, err := amqp.ParseURI(q.amqpURL())
			if err != nil {
				t.Fatalf("built URL does not parse: %v", err)
			}

			if uri.Vhost != tt.want {
				t.Errorf("vhost = %q, want %q", uri.Vhost, tt.want)
			}
		})
	}
}

func TestRabbitMQ_amqpURLKeepsConfiguredURL(t *testing.T) {
	raw := "amqps://u:p@host:5671/v"

	q, err := NewRabbitMQQueue(Config{URL: raw, Username: "ignored"}, forge.NewNoopLogger(), forge.NewNoOpMetrics())
	if err != nil {
		t.Fatalf("NewRabbitMQQueue() error = %v", err)
	}

	if got := q.amqpURL(); got != raw {
		t.Errorf("amqpURL() = %q, want %q", got, raw)
	}
}

func TestAMQPLogFieldsOmitPassword(t *testing.T) {
	q, err := NewRabbitMQQueue(Config{
		Hosts:    []string{"localhost:5672"},
		Username: "guest",
		Password: trickyPassword,
	}, forge.NewNoopLogger(), forge.NewNoOpMetrics())
	if err != nil {
		t.Fatalf("NewRabbitMQQueue() error = %v", err)
	}

	uri, err := amqp.ParseURI(q.amqpURL())
	if err != nil {
		t.Fatalf("built URL does not parse: %v", err)
	}

	logger := newCaptureLogger()
	logger.Info("connected to rabbitmq", amqpLogFields(uri)...)

	got, _ := logger.line("connected to rabbitmq")
	if strings.Contains(got, trickyPassword) {
		t.Errorf("log line carries the password: %s", got)
	}

	for _, want := range []string{"host=localhost", "port=5672", "vhost=/"} {
		if !strings.Contains(got, want) {
			t.Errorf("log line %q is missing %q", got, want)
		}
	}
}

// A URL that fails to parse must not come back quoted in the error: net/url
// quotes the whole thing, password and all. These fail before any dial, so
// they need no broker.
func TestConnect_BadURLErrorOmitsPassword(t *testing.T) {
	const secret = "s3cretPW"

	newQueue := map[string]func(Config) (Queue, error){
		"rabbitmq": func(c Config) (Queue, error) {
			return NewRabbitMQQueue(c, forge.NewNoopLogger(), forge.NewNoOpMetrics())
		},
		"nats": func(c Config) (Queue, error) {
			return NewNATSQueue(c, forge.NewNoopLogger(), forge.NewNoOpMetrics())
		},
		"redis": func(c Config) (Queue, error) {
			return NewRedisQueue(c, forge.NewNoopLogger(), forge.NewNoOpMetrics())
		},
	}

	tests := []struct {
		name   string
		driver string
		config Config
	}{
		{"rabbitmq url", "rabbitmq", Config{URL: "amqp://guest:" + secret + "%zz@localhost:5672/"}},
		{"rabbitmq hosts", "rabbitmq", Config{Hosts: []string{"bad host"}, Username: "guest", Password: secret}},
		{"nats url", "nats", Config{URL: "nats://user:" + secret + "%zz@localhost:4222", ConnectTimeout: 1}},
		{"redis url", "redis", Config{URL: "redis://user:" + secret + "%zz@localhost:6379"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, err := newQueue[tt.driver](tt.config)
			if err != nil {
				t.Fatalf("constructor error = %v", err)
			}

			err = q.Connect(context.Background())
			if err == nil {
				t.Fatal("Connect() succeeded on a malformed URL")
			}

			if strings.Contains(err.Error(), secret) {
				t.Errorf("error carries the password: %v", err)
			}
		})
	}
}

func TestRedactURL(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{"amqp://guest:hunter2@localhost:5672/prod", "amqp://guest:xxxxx@localhost:5672/prod"},
		{"redis://localhost:6379/0", "redis://localhost:6379/0"},
		{"localhost:6379", "localhost:6379"},
		{"amqp://guest:hun%zzter2@localhost/", "amqp://xxxxx@localhost/"},
		{"user:hunter2@host", "xxxxx@host"},
	}

	for _, tt := range tests {
		if got := redactURL(tt.raw); got != tt.want {
			t.Errorf("redactURL(%q) = %q, want %q", tt.raw, got, tt.want)
		}
	}

	got := redactURLList("nats://a:pw1@h1:4222, nats://b:pw2@h2:4222")
	if want := "nats://a:xxxxx@h1:4222,nats://b:xxxxx@h2:4222"; got != want {
		t.Errorf("redactURLList() = %q, want %q", got, want)
	}
}

// TestRabbitMQ_ConnectLogOmitsPassword drives a real Connect against a broker
// whose password needs escaping. Point it at one with:
//
//	QUEUE_TEST_RABBITMQ_HOST=localhost:5672
//	QUEUE_TEST_RABBITMQ_USER=...
//	QUEUE_TEST_RABBITMQ_PASS=...
func TestRabbitMQ_ConnectLogOmitsPassword(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	host := os.Getenv("QUEUE_TEST_RABBITMQ_HOST")
	if host == "" {
		t.Skip("QUEUE_TEST_RABBITMQ_HOST not set")
	}

	pass := os.Getenv("QUEUE_TEST_RABBITMQ_PASS")
	logger := newCaptureLogger()

	q, err := NewRabbitMQQueue(Config{
		Hosts:    []string{host},
		Username: os.Getenv("QUEUE_TEST_RABBITMQ_USER"),
		Password: pass,
	}, logger, forge.NewNoOpMetrics())
	if err != nil {
		t.Fatalf("NewRabbitMQQueue() error = %v", err)
	}

	if err := q.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer q.Disconnect(context.Background())

	got, ok := logger.line("connected to rabbitmq")
	if !ok {
		t.Fatal("no connect log line")
	}

	if pass != "" && strings.Contains(got, pass) {
		t.Errorf("log line carries the password: %s", got)
	}
}
