package main

import (
	"crypto/rand"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"net/http"
	"encoding/json"
	"strconv"
	"os/exec"

	"github.com/codahale/hdrhistogram"
	"github.com/nats-io/go-nats"
	"github.com/tylertreat/hdrhistogram-writer"
)

// Test Parameters
var (
	ServerA       string
	ServerB       string
	TargetPubRate int
	MsgSize       int
	NumPubs       int
	TestDuration  time.Duration
	HistFile      string
	Secure        bool
	TLSca         string
	TLSkey        string
	TLScert       string
	Port          string
	Subjects      int
	Publishers    string
	Subscribers   string
	MsgSizeBench  string
)

var usageStr = `
Usage: latency-tests [options]

Test Options:
    -sa <url>        ServerA (Publish) (default: nats://localhost:4222)
    -sb <url>        ServerB (Subscribe) (default: nats://localhost:4222)
    -sz <int>        Message size in bytes (default: 8)
    -tr <int>        Rate in msgs/sec (default: 1000)
    -tt <string>     Test duration (default: 5s)
    -hist <file>     Histogram output file
    -secure          Enable TLS without verfication (default: false)
    -tls_ca <string> TLS Certificate CA file
    -tls_key <file>  TLS Private Key
    -tls_cert <file> TLS Certificate
    -port <int>      API port (default: 9080)
    -subjects <int>  Number of subjects (default: 25)
`

func usage() {
	log.Fatalf(usageStr + "\n")
}

// waitForRoute tests a subscription in the server to ensure subject interest
// has been propagated between servers.  Otherwise, we may miss early messages
// when testing with clustered servers and the test will hang.
func waitForRoute(pnc, snc *nats.Conn) {

	// No need to continue if using one server
	if strings.Compare(pnc.ConnectedServerId(), snc.ConnectedServerId()) == 0 {
		return
	}

	// Setup a test subscription to let us know when a message has been received.
	// Use a new inbox subject as to not skew results
	var routed int32
	subject := nats.NewInbox()
	sub, err := snc.Subscribe(subject, func(msg *nats.Msg) {
		atomic.AddInt32(&routed, 1)
	})
	if err != nil {
		log.Fatalf("Couldn't subscribe to test subject %s: %v", subject, err)
	}
	defer sub.Unsubscribe()
	snc.Flush()

	// Periodically send messages until the test subscription receives
	// a message.  Allow for two seconds.
	start := time.Now()
	for atomic.LoadInt32(&routed) == 0 {
		if time.Since(start) > (time.Second * 2) {
			log.Fatalf("Couldn't receive end-to-end test message.")
		}
		if err = pnc.Publish(subject, nil); err != nil {
			log.Fatalf("Couldn't publish to test subject %s:  %v", subject, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func main() {
	start := time.Now()

	flag.StringVar(&ServerA, "sa", nats.DefaultURL, "ServerA - Publisher")
	flag.StringVar(&ServerB, "sb", nats.DefaultURL, "ServerB - Subscriber")
	flag.IntVar(&TargetPubRate, "tr", 1000, "Target Publish Rate")
	flag.IntVar(&MsgSize, "sz", 8, "Message Payload Size")
	flag.DurationVar(&TestDuration, "tt", 5*time.Second, "Target Test Time")
	flag.StringVar(&HistFile, "hist", "", "Histogram and Raw Output")
	flag.BoolVar(&Secure, "secure", false, "Use a TLS Connection w/o verification")
	flag.StringVar(&TLSkey, "tls_key", "", "Private key file")
	flag.StringVar(&TLScert, "tls_cert", "", "Certificate file")
	flag.StringVar(&TLSca, "tls_ca", "", "Certificate CA file")
	flag.StringVar(&Port, "port", "9080", "API Port")
	flag.IntVar(&Subjects, "subjects", 25, "Number of subjects")
	flag.StringVar(&Publishers, "publishers", "10", "Number of publishers")
	flag.StringVar(&Subscribers, "subscribers", "10", "Number of subscribers")
	flag.StringVar(&MsgSizeBench, "msBench", "8", "Message Payload Size for nats-benchmark")

	log.SetFlags(0)
	flag.Usage = usage
	flag.Parse()

	NumPubs = int(TestDuration/time.Second) * TargetPubRate

	if MsgSize < 8 {
		log.Fatalf("Message Payload Size must be at least %d bytes\n", 8)
	}

	// Setup connection options
	var opts []nats.Option
	if Secure {
		opts = append(opts, nats.Secure())
	}
	if TLSca != "" {
		opts = append(opts, nats.RootCAs(TLSca))
	}
	if TLScert != "" {
		opts = append(opts, nats.ClientCert(TLScert, TLSkey))
	}

	c1, err := nats.Connect(ServerA, opts...)
	if err != nil {
		log.Fatalf("Could not connect to ServerA: %v", err)
	}
	c2, err := nats.Connect(ServerB, opts...)
	if err != nil {
		log.Fatalf("Could not connect to ServerB: %v", err)
	}

	// Do some quick RTT calculations
	log.Println("==============================")
	now := time.Now()
	c1.Flush()
	log.Printf("Pub Server RTT : %v\n", fmtDur(time.Since(now)))

	now = time.Now()
	c2.Flush()
	log.Printf("Sub Server RTT : %v\n", fmtDur(time.Since(now)))

	// Duration tracking
	durations := make([]time.Duration, 0, NumPubs)
	latestDurations := make([]time.Duration, 0, NumPubs)

	// Lock mechanism for latestDurations
	var mutex = &sync.Mutex{}

	// variable for dynamic metrics
	var lastMetrics = make(map[string]float64)

	// Wait for all messages to be received.
	var wg sync.WaitGroup
	wg.Add(1)

	//Random subject (to run multiple tests in parallel)
	subject := nats.NewInbox()

	// Count the messages.
	received := 0

	// Async Subscriber (Runs in its own Goroutine)
	c2.Subscribe(subject, func(msg *nats.Msg) {
		sendTime := int64(binary.LittleEndian.Uint64(msg.Data))
		durations = append(durations, time.Duration(time.Now().UnixNano()-sendTime))
		mutex.Lock()
		latestDurations = append(latestDurations, time.Duration(time.Now().UnixNano()-sendTime))
		mutex.Unlock()
		received++
		if received >= NumPubs {
			wg.Done()
		}
	})
	// Make sure interest is set for subscribe before publish since a different connection.
	c2.Flush()

	// wait for routes to be established so we get every message
	waitForRoute(c1, c2)

	log.Printf("Message Payload: %v\n", byteSize(MsgSize))
	log.Printf("Target Duration: %v\n", TestDuration)
	log.Printf("Target Msgs/Sec: %v\n", TargetPubRate)
	log.Printf("Target Band/Sec: %v\n", byteSize(TargetPubRate*MsgSize*2))
	log.Println("==============================")

	// Random payload
	data := make([]byte, MsgSize)
	io.ReadFull(rand.Reader, data)

	// fixed percentiles values
	percentiles := []float64{10, 50, 75, 90, 99, 99.99, 99.999, 99.9999, 99.99999, 100.0}

	// Set ticker to print histogram dynamically
	ticker := time.NewTicker(3000 * time.Millisecond)
	stop := make(chan bool, 1)
	go func() {
		for {
			select {
			case <-ticker.C:
				mutex.Lock()
				sort.Slice(latestDurations, func(i, j int) bool { return latestDurations[i] < latestDurations[j] })

				if len(latestDurations) == 0 {
					continue
				}

				h := hdrhistogram.New(1, int64(latestDurations[len(latestDurations)-1]), 5)
				for _, d := range latestDurations {
					h.RecordValue(int64(d))
				}

				avg_latency := averageLatency(latestDurations)
				latestDurations = make([]time.Duration, 0, NumPubs)
				mutex.Unlock()

				lastMetrics["AverageLatency"] = avg_latency
				log.Printf("HDR Percentiles:\n")

				for _, percentile := range percentiles {
					lastMetrics["Percentile" + fmt.Sprintf("%.5f", percentile)] = float64(time.Duration(h.ValueAtQuantile(percentile)).Nanoseconds())/1000000.0
					log.Printf("%.5f:       %v\n", percentile, fmtDur(time.Duration(h.ValueAtQuantile(percentile))))
				}
				log.Printf("AverageLatency: %v ms\n\n", avg_latency)
				log.Println("==============================")

			case <-stop:
				log.Println("EXIT")
				return
			}
		}
	}()

	// For publish throttling
	delay := time.Second / time.Duration(TargetPubRate)
	pubStart := time.Now()

	// Throttle logic, crude I know, but works better then time.Ticker.
	adjustAndSleep := func(count int) {
		r := rps(count, time.Since(pubStart))
		adj := delay / 20 // 5%
		if adj == 0 {
			adj = 1 // 1ns min
		}
		if r < TargetPubRate {
			delay -= adj
		} else if r > TargetPubRate {
			delay += adj
		}
		if delay < 0 {
			delay = 0
		}
		time.Sleep(delay)
	}

	for i := 0; i < Subjects; i++ {
		topic := "foo" + strconv.Itoa(i)
		go func() {
			cmd := exec.Command("../../../../bin/nats-bench", "-np", Publishers, "-ns", Subscribers, "-n", "10000000000000", "-ms", MsgSizeBench, topic)
			log.Printf("Running nats-bench and waiting for it to finish... ")
			err := cmd.Run()
			log.Printf("Command finished with error: %v", err)
		}()
	}

	go func() {
		// Now publish
		for i := 0; i < NumPubs; i++ {
			now := time.Now()
			// Place the send time in the front of the payload.
			binary.LittleEndian.PutUint64(data[0:], uint64(now.UnixNano()))
			c1.Publish(subject, data)
			adjustAndSleep(i + 1)
		}
		pubDur := time.Since(pubStart)
		wg.Wait()
		subDur := time.Since(pubStart)
		ticker.Stop()
		stop <- true
		time.Sleep(1000 * time.Millisecond)

		// If we are writing to files, save the original unsorted data
		if HistFile != "" {
			if err := writeRawFile(HistFile+".raw", durations); err != nil {
				log.Printf("Unable to write raw output file: %v", err)
			}
		}

		sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })

		h := hdrhistogram.New(1, int64(durations[len(durations)-1]), 5)
		for _, d := range durations {
			h.RecordValue(int64(d))
		}

		log.Printf("HDR Percentiles:\n")
		for _, percentile := range percentiles {
			log.Printf("%.4f:       %v\n", percentile, fmtDur(time.Duration(h.ValueAtQuantile(percentile))))
		}
		log.Println("==============================")

		if HistFile != "" {
			pctls := histwriter.Percentiles{10, 25, 50, 75, 90, 99, 99.9, 99.99, 99.999, 99.9999, 99.99999, 100.0}
			histwriter.WriteDistributionFile(h, pctls, 1.0/1000000.0, HistFile+".histogram")
		}

		// Print results
		log.Printf("Actual Msgs/Sec: %d\n", rps(NumPubs, pubDur))
		log.Printf("Actual Band/Sec: %v\n", byteSize(rps(NumPubs, pubDur)*MsgSize*2))
		log.Printf("Minimum Latency: %v", fmtDur(durations[0]))
		log.Printf("Median Latency : %v", fmtDur(getMedian(durations)))
		log.Printf("Maximum Latency: %v", fmtDur(durations[len(durations)-1]))
		log.Printf("1st Sent Wall Time : %v", fmtDur(pubStart.Sub(start)))
		log.Printf("Last Sent Wall Time: %v", fmtDur(pubDur))
		log.Printf("Last Recv Wall Time: %v", fmtDur(subDur))
	}()

	http.HandleFunc("/latency_stats", func(w http.ResponseWriter, r *http.Request) {
		data, _ := json.Marshal(lastMetrics)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(data)
	})

	log.Fatal(http.ListenAndServe(":" + Port, nil))
}

const fsecs = float64(time.Second)

func rps(count int, elapsed time.Duration) int {
	return int(float64(count) / (float64(elapsed) / fsecs))
}

// Just pretty print the byte sizes.
func byteSize(n int) string {
	sizes := []string{"B", "K", "M", "G", "T"}
	base := float64(1024)
	if n < 10 {
		return fmt.Sprintf("%d%s", n, sizes[0])
	}
	e := math.Floor(logn(float64(n), base))
	suffix := sizes[int(e)]
	val := math.Floor(float64(n)/math.Pow(base, e)*10+0.5) / 10
	f := "%.0f%s"
	if val < 10 {
		f = "%.1f%s"
	}
	return fmt.Sprintf(f, val, suffix)
}

func logn(n, b float64) float64 {
	return math.Log(n) / math.Log(b)
}

// Make time durations a bit prettier.
func fmtDur(t time.Duration) time.Duration {
	// e.g 234us, 4.567ms, 1.234567s
	return t.Truncate(time.Microsecond)
}

func getMedian(values []time.Duration) time.Duration {
	l := len(values)
	if l == 0 {
		log.Fatalf("empty set")
	}
	if l%2 == 0 {
		return (values[l/2-1] + values[l/2]) / 2
	}
	return values[l/2]
}

// writeRawFile creates a file with a list of recorded latency
// measurements, one per line.
func writeRawFile(filePath string, values []time.Duration) error {
	f, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer f.Close()
	for _, value := range values {
		fmt.Fprintf(f, "%f\n", float64(value.Nanoseconds())/1000000.0)
	}
	return nil
}

// averageLatency calculates the average of a list of recorded latency
// measurements in msec
func averageLatency(values []time.Duration) float64 {
        sum := 0.0
        for _, value := range values {
	        sum += float64(value.Nanoseconds())/1000000.0
	}
	return sum / float64(len(values))
}
