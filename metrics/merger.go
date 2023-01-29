package metrics

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/go-co-op/gocron"
	promClient "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/rs/zerolog"
	"golang.org/x/exp/maps"
	"google.golang.org/protobuf/proto"
)

type IMerger interface {
	Add(pluginName string, unixDomainSocket string)
	ReadMetrics() (map[string][]byte, error)
	Start()
	Stop()
}

type Merger struct {
	metricsMergerScheduler *gocron.Scheduler

	Logger              zerolog.Logger
	MetricsMergerPeriod time.Duration
	Addresses           map[string]string
	OutputMetrics       []byte
}

var _ IMerger = &Merger{}

// NewMerger creates a new metrics merger.
func NewMerger(metricsMergerPeriod time.Duration, logger zerolog.Logger) *Merger {
	return &Merger{
		metricsMergerScheduler: gocron.NewScheduler(time.UTC),
		Logger:                 logger,
		Addresses:              map[string]string{},
		OutputMetrics:          []byte{},
		MetricsMergerPeriod:    metricsMergerPeriod,
	}
}

// Add adds a plugin and its unix domain socket to the map of plugins to merge metrics from.
func (m *Merger) Add(pluginName string, unixDomainSocket string) {
	if _, ok := m.Addresses[pluginName]; ok {
		m.Logger.Warn().Fields(
			map[string]interface{}{
				"plugin": pluginName,
				"socket": unixDomainSocket,
			}).Msg("Plugin already registered")
		return
	}
	m.Addresses[pluginName] = unixDomainSocket
}

// ReadMetrics reads metrics from plugins by reading from their unix domain sockets.
//
//nolint:wrapcheck
func (m *Merger) ReadMetrics() (map[string][]byte, error) {
	readers := make(map[string][]byte)

	for pluginName, unixDomainSocket := range m.Addresses {
		if file, err := os.Stat(unixDomainSocket); err != nil || file.IsDir() || file.Mode().Type() != os.ModeSocket {
			continue
		}

		NewHTTPClientOverUDS := func(unixDomainSocket string) http.Client {
			return http.Client{
				Transport: &http.Transport{
					DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
						var d net.Dialer
						return d.DialContext(ctx, "unix", unixDomainSocket)
					},
				},
			}
		}

		client := NewHTTPClientOverUDS(unixDomainSocket)
		request, err := http.NewRequestWithContext(
			context.Background(), http.MethodGet, "http://plugins/metrics", nil)
		if err != nil {
			return nil, err
		}

		response, err := client.Do(request)
		if err != nil {
			return nil, err
		}
		defer response.Body.Close()

		metrics, err := io.ReadAll(response.Body)
		if err != nil {
			return nil, err
		}

		readers[pluginName] = metrics
	}

	return readers, nil
}

// Start starts the metrics merger.
func (m *Merger) Start() {
	// Merge metrics from plugins by reading from their unix domain sockets.
	// This is done periodically.

	if _, err := m.metricsMergerScheduler.
		Every(m.MetricsMergerPeriod).
		SingletonMode().
		StartAt(time.Now().Add(m.MetricsMergerPeriod)).
		Do(func() {
			pluginMetrics, err := m.ReadMetrics()
			if err != nil {
				m.Logger.Error().Err(err).Msg("Failed to read plugin metrics")
				return
			}

			// TODO: There should be a better, more efficient way to merge metrics from plugins.
			var metricsOutput bytes.Buffer
			enc := expfmt.NewEncoder(io.Writer(&metricsOutput), expfmt.FmtText)
			for pluginName, metrics := range pluginMetrics {
				if metrics == nil {
					m.Logger.Trace().Str("plugin", pluginName).Msg("Plugin metrics are empty")
					continue
				}

				// Retrieve plugin metrics.
				textParser := expfmt.TextParser{}
				reader := bytes.NewReader(metrics)
				metrics, err := textParser.TextToMetricFamilies(reader)
				if err != nil {
					m.Logger.Error().Err(err).Msg("Failed to parse plugin metrics")
					continue
				}

				metricFamilies := map[string]*promClient.MetricFamily{}
				for _, metric := range metrics {
					for _, sample := range metric.Metric {
						// Add plugin label to each metric.
						sample.Label = append(sample.Label, &promClient.LabelPair{
							Name:  proto.String("plugin"),
							Value: proto.String(strings.ReplaceAll(pluginName, "-", "_")),
						})
					}
					metricFamilies[metric.GetName()] = metric
				}

				metricNames := maps.Keys(metricFamilies)
				sort.Strings(metricNames)
				for _, metric := range metricNames {
					err := enc.Encode(metricFamilies[metric])
					if err != nil {
						m.Logger.Error().Err(err).Msg("Failed to encode plugin metrics")
						return
					}
				}

				m.Logger.Debug().Fields(
					map[string]interface{}{
						"plugin": pluginName,
						"count":  len(metricNames),
					}).Msgf("Processed and merged metrics")
			}

			m.OutputMetrics = metricsOutput.Bytes()
		}); err != nil {
		m.Logger.Error().Err(err).Msg("Failed to start metrics merger scheduler")
	}

	m.metricsMergerScheduler.StartAsync()
}

// Stop stops the metrics merger.
func (m *Merger) Stop() {
	m.metricsMergerScheduler.Clear()
}
