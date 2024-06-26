package listener

import (
	"druid-exporter/utils"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/golang/gddo/httputil/header"
	"github.com/patrickmn/go-cache"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
)

// DruidHTTPEndpoint is the endpoint to listen all druid metrics
func DruidHTTPEndpoint(metricsCleanupTTL int, disableHistogram bool, histogram *prometheus.HistogramVec, gauge *prometheus.GaugeVec, dnsCache *cache.Cache) http.HandlerFunc {
	gaugeCleaner := newCleaner(gauge, metricsCleanupTTL)
	return func(w http.ResponseWriter, req *http.Request) {
		var druidData []map[string]interface{}
		var id string
		reqHeader, _ := header.ParseValueAndParams(req.Header, "Content-Type")
		if req.Method == "POST" && reqHeader == "application/json" {
			output, err := io.ReadAll(req.Body)
			defer req.Body.Close()
			if err != nil {
				logrus.Debugf("Unable to read JSON response: %v", err)
				return
			}
			err = json.Unmarshal(output, &druidData)
			if err != nil {
				logrus.Errorf("Error decoding JSON sent by druid: %v", err)
				if druidData != nil {
					logrus.Debugf("%v", druidData)
				}
				return
			}
			if druidData == nil {
				logrus.Debugf("The dataset for druid is empty, can be ignored: %v", druidData)
				return
			}
			for i, data := range druidData {
				metric := fmt.Sprintf("%v", data["metric"])
				service := fmt.Sprintf("%v", data["service"])
				hostname := fmt.Sprintf("%v", data["host"])
				datasource := data["dataSource"]
				value, _ := strconv.ParseFloat(fmt.Sprintf("%v", data["value"]), 64)

				if data["id"] != nil {
					id = fmt.Sprintf("%v", data["id"])
				} else {
					id = ""
				}
				// Reverse DNS Lookup
				// Mutates dnsCache
				hostValue := strings.Split(hostname, ":")[0]
				dnsLookupValue := utils.ReverseDNSLookup(hostValue, dnsCache)

				host := strings.Replace(hostname, hostValue, dnsLookupValue, 1) // Adding back port

				if i == 0 { // Comment out this line if you want the whole metrics received
					logrus.Tracef("parameters received and mapped:")
					logrus.Tracef("    metric     => %s", metric)
					logrus.Tracef("    service    => %s", service)
					logrus.Tracef("    hostname   => (%s -> %s)", hostname, host)
					logrus.Tracef("    datasource => %v", datasource)
					logrus.Tracef("    value      => %v", value)
					logrus.Tracef("    id         => %v", id)
				}

				if data["dataSource"] != nil {
					if arrDatasource, ok := datasource.([]interface{}); ok { // Array datasource
						for _, entryDatasource := range arrDatasource {
							if !disableHistogram {
								histogram.With(prometheus.Labels{
									"metric_name": strings.Replace(metric, "/", "-", 3),
									"service":     strings.Replace(service, "/", "-", 3),
									"datasource":  entryDatasource.(string),
									"host":        host,
									"id":          id,
								}).Observe(value)
							}

							labels := prometheus.Labels{
								"metric_name": strings.Replace(metric, "/", "-", 3),
								"service":     strings.Replace(service, "/", "-", 3),
								"datasource":  entryDatasource.(string),
								"host":        host,
								"id":          id,
							}
							gaugeCleaner.add(labels)
							gauge.With(labels).Set(value)
						}
					} else { // Single datasource
						if !disableHistogram {
							histogram.With(prometheus.Labels{
								"metric_name": strings.Replace(metric, "/", "-", 3),
								"service":     strings.Replace(service, "/", "-", 3),
								"datasource":  datasource.(string),
								"host":        host,
								"id":          id,
							}).Observe(value)
						}

						labels := prometheus.Labels{
							"metric_name": strings.Replace(metric, "/", "-", 3),
							"service":     strings.Replace(service, "/", "-", 3),
							"datasource":  datasource.(string),
							"host":        host,
							"id":          id,
						}
						gaugeCleaner.add(labels)
						gauge.With(labels).Set(value)
					}
				} else { // Missing datasource case
					if !disableHistogram {
						histogram.With(prometheus.Labels{
							"metric_name": strings.Replace(metric, "/", "-", 3),
							"service":     strings.Replace(service, "/", "-", 3),
							"datasource":  "",
							"host":        host,
							"id":          id,
						}).Observe(value)
					}

					labels := prometheus.Labels{
						"metric_name": strings.Replace(metric, "/", "-", 3),
						"service":     strings.Replace(service, "/", "-", 3),
						"datasource":  "",
						"host":        host,
						"id":          id,
					}
					gaugeCleaner.add(labels)
					gauge.With(labels).Set(value)
				}
			}

			logrus.Infof("Successfully collected data from druid emitter, %s", druidData[0]["service"].(string))
		}
	}
}
