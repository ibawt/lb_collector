package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"cloud.google.com/go/pubsub"
	"github.com/DataDog/datadog-go/statsd"
	log "github.com/Sirupsen/logrus"
	"golang.org/x/net/context"
	_ "golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
)

// LoadBalancerCollector ...
type LoadBalancerCollector struct {
	statsdClient *statsd.Client
	project      string
}

// LogEntry ...
type LogEntry struct {
	Metadata      interface{} `json:"metadata"`
	InsertID      string      `json:"insertId"`
	Log           string      `json:"log"`
	StructPayload interface{} `json:"structPayload"`
	HTTPRequest   HTTPRequest `json:"httpRequest"`
}

// HTTPRequest ...
type HTTPRequest struct {
	RequestMethod string `json:"requestMethod"`
	RequestURL    string `json:"requestUrl"`
	RequestSize   string `json:"requestSize"`
	Status        int    `json:"status"`
	ResponseSize  string `json:"responseSize"`
	RemoteIP      string `json:"remoteIp"`
	ServerIP      string `json:"serverIp"`
}

func getBaseHost(requestURL string) string {
	url, err := url.Parse(requestURL)
	if err != nil {
		return "unknown"
	}

	return url.Host
}

func (lbc *LoadBalancerCollector) emitMetric(h *HTTPRequest) error {
	tags := []string{
		fmt.Sprintf("status:%d", h.Status),
		fmt.Sprintf("hostname:%s", getBaseHost(h.RequestURL)),
	}

	log.WithField("status", h.Status).WithField("hostname", h.RequestURL).Info("emit")
	return lbc.statsdClient.Incr("glbc.http.request", tags, 1.0)
}

func (lbc *LoadBalancerCollector) listen() error {
	ctx := context.Background()

	ts, err := google.DefaultTokenSource(ctx, pubsub.ScopePubSub)
	if err != nil {
		return err
	}
	client, err := pubsub.NewClient(ctx, lbc.project, option.WithTokenSource(ts))
	if err != nil {
		return err
	}

	topic := client.Topic("loadbalancer-logs") //

	subs, err := client.CreateSubscription(ctx, "lb-collector", topic, 1*time.Minute, nil)
	if err != nil {
		subs = client.Subscription("lb-collector")
	}

	it, err := subs.Pull(ctx)
	if err != nil {
		return err
	}

	log.Info("Listening for subscribed messages...")

	defer it.Stop()

	for {
		msg, err := it.Next()
		if err == iterator.Done {
			return nil
		}
		if err != nil {
			log.WithError(err).Warn("Error in it.Next()")
		} else {
			if msg.PublishTime.After(time.Now().Add(-1 * time.Minute)) {
				decoder := json.NewDecoder(bytes.NewBuffer(msg.Data))
				var entry LogEntry

				err = decoder.Decode(&entry)
				if err != nil {
					log.WithError(err).Warn("error in decode")
					continue
				}
				err = lbc.emitMetric(&entry.HTTPRequest)
				if err != nil {
					log.WithError(err).Warn("Error in metric emission")
				}
			}
			msg.Done(true)
		}
	}
}

func (lbc *LoadBalancerCollector) healthCheckListener(url string) {
	http.HandleFunc("/services/ping", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte("PONG\n"))
		if err != nil {
			log.WithError(err).Warn("Writing health check response")
		}
	})
	err := http.ListenAndServe(url, nil)
	if err != nil {
		log.WithError(err).Fatal("Healthcheck listener failed!")
	}
}

func main() {
	var statsdEndpoint = flag.String("statsd", "localhost:8125", "StatsD endpoint")
	var project = flag.String("project", "", "Project ID")
	var httpServer = flag.String("http", ":8080", "Default host:port for health checks")
	var bufferLength = flag.Int("buffer", 256, "Amount of statsd messages to buffer")
	flag.Parse()

	if *project == "" {
		log.Fatalln("Project must be specified!")
	}

	log.Info("about to make statsd client")
	sd, err := statsd.NewBuffered(*statsdEndpoint, *bufferLength)
	if err != nil {
		log.WithError(err).Fatal("Couldn't create StatsD Client")
	}

	lbCollector := LoadBalancerCollector{
		statsdClient: sd,
		project:      *project,
	}

	go lbCollector.healthCheckListener(*httpServer)
	log.WithField("HealthCheckAddress", *httpServer).Info("HealthCheck listening...")

	err = lbCollector.listen()
	if err != nil {
		log.WithError(err).Fatal("Error in Listen")
	}
}
