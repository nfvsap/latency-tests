package main

import (
	"flag"
	"log"
	"fmt"
	"os"
	"bufio"
	"strings"
	"net/http"
	"encoding/json"
	"strconv"
	"io/ioutil"
	"os/exec"
)

// Test Parameters
var (
	Connections   int
	Port          string
	MetricsFile   string
	Workers       int
	IP            string
	TargetFile    string
)

var usageStr = `
Usage: nginx [options]

Test Options:
    -connections <int>    Number of connections (default: 10)
    -port <int>           Metrics port (default: 9080)
    -workers <int>        Number of workers (default: 10)
    -ip <string>          Nginx proxy server IP address (default: 0.0.0.0:80)
    -metricsFile <string> File to store the metrics (default: metrics.txt)
    -targetFile <string>  Requested file from client (default: 1kb.bin)
`

func usage() {
	log.Fatalf(usageStr + "\n")
}


func main() {
	flag.IntVar(&Connections, "connections", 10, "Number of connections")
	flag.StringVar(&Port, "port", "9080", "Metrics Port")
	flag.StringVar(&MetricsFile, "metricsFile", "metrics.txt", "File to store the metrics")
	flag.IntVar(&Workers, "workers", 10, "Number of workers")
	flag.StringVar(&IP, "ip", "0.0.0.0:80", "Nginx proxy server IP address")
	flag.StringVar(&TargetFile, "targetFile", "1kb.bin", "Requested file from client")

	log.SetFlags(0)
	flag.Usage = usage
	flag.Parse()



	for i := 0; i < Workers; i++ {
		go func() {
			cmd := exec.Command("wrk", "-t", "1", "-c", strconv.Itoa(Connections), "-d", "40h", "--latency", "http://" + IP + "/" + TargetFile)
			log.Printf("Running wrk and waiting for it to finish... ")
			err := cmd.Run()
			log.Printf("Command finished with error: %v", err)
		}()
	}


	go func() {
		for {
			output, err := exec.Command("wrk", "-t", "1", "-c", strconv.Itoa(Connections), "-d", "3s", "--latency", "http://" + IP + "/" + TargetFile).Output()
			if err != nil {
				log.Printf("Command finished with error: %v", err)
			}
			err = ioutil.WriteFile(MetricsFile, output, 0644)
			if err != nil {
				log.Printf("Writing file failed with error: %v", err)
			}
		}
	}()

	percentiles := []string{"50%", "75%", "90%", "99%"}
	http.HandleFunc("/latency_stats", func(w http.ResponseWriter, r *http.Request) {
		f, err := os.Open(MetricsFile)
		if err != nil {
			panic(err)
		}
		fs := bufio.NewScanner(f)
		lateMap := make(map[string]float64)
		for fs.Scan() {
			txt := fs.Text()
			if strings.Contains(txt, "Running") || strings.Contains(txt, "threads") || strings.Contains(txt, "Thread") || strings.Contains(txt, "Distribution") || strings.Contains(txt, "requests") || strings.Contains(txt, "Requests") || strings.Contains(txt, "Transfer") {
				continue
			}
			splitted := strings.Split(txt," ")
			var slc []string
			for _, str := range splitted {
				if str != ""{
					slc = append(slc, str)
				}
			}

			if strings.Contains(txt, "Latency") {
				lateMap["AvgLatency"] = convertToMS(slc[1])
			} else if strings.Contains(txt, "Req/Sec") {
				lateMap["AvgThroughput"] = convertToK(slc[1])
			} else {
				for i:=0; i < len(percentiles); i++ {
					if strings.Contains(txt, percentiles[i]){
						lateMap["Percentile" + percentiles[i]] = convertToMS(slc[1])
						break
					}
				}
			}
		}

		data, _ := json.Marshal(lateMap)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(data)
	})

	log.Fatal(http.ListenAndServe(":" + Port, nil))
}

func convertToMS(metric string) float64 {
	var s float64
	var err error

	if strings.Contains(metric, "us") {
		number := strings.Split(metric,"us")[0]
		s, err = strconv.ParseFloat(number, 64)
		s = s / 1000.0
	} else if strings.Contains(metric, "ns") {
		number := strings.Split(metric,"ns")[0]
		s, err = strconv.ParseFloat(number, 64)
		s = s / 1000000.0
	} else if strings.Contains(metric, "ms") {
		number := strings.Split(metric,"ms")[0]
		s, err = strconv.ParseFloat(number, 64)
	} else if strings.Contains(metric, "s") {
		number := strings.Split(metric,"s")[0]
		s, err = strconv.ParseFloat(number, 64)
		s = s * 1000.0
	} else {
		err = fmt.Errorf("No known metric")
	}

	if err != nil {
		panic(err)
	}

	return s
}

func convertToK(metric string) float64 {
	if strings.Contains(metric, "k") {
		number := strings.Split(metric,"k")[0]
		s, err := strconv.ParseFloat(number, 64)
		s = s * 1000.0

		if  err != nil {
			panic(err)
		}
		return s
	} else {
		panic("No known metric")
	}
}
