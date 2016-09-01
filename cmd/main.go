package main

import (
	"flag"
	"fmt"
	"github.com/atercattus/h2client"
	"github.com/pkg/errors"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"runtime/debug"
	"runtime/pprof"
	"strconv"
	"sync/atomic"
	"syscall"
	"time"
)

var (
	args struct {
		Host string
	}
)

func init() {
	flag.StringVar(&args.Host, `host`, `ater.me`, `Host for connect to`)
	flag.Parse()
}

var reqIdx uint32

func main() {
	if true {
		debug.SetGCPercent(100)
	}

	go func() {
		http.ListenAndServe(`:1935`, nil)
	}()

	if false {
		pidStr := strconv.Itoa(os.Getpid())
		if f, err := os.Create(`./prof.` + pidStr + `.cpu`); err == nil {
			pprof.StartCPUProfile(f)
		}
		defer func() {
			if f, err := os.Create(`./prof.` + pidStr + `.mem`); err == nil {
				pprof.WriteHeapProfile(f)
			}
			pprof.StopCPUProfile()
		}()
	}

	go func() {
		for {
			now := time.Now().UnixNano()
			idxPrev := atomic.LoadUint32(&reqIdx)
			time.Sleep(1 * time.Second)
			qps := float64(reqIdx-idxPrev) / (float64(time.Now().UnixNano()-now) / float64(time.Second))
			fmt.Printf("%.2f qps\n", qps)
		}
	}()

	if false {
		simpleTest()
	} else {
		//stressTest()
		//stressTest2()
		stressTest3()

		chSignal := make(chan os.Signal, 2)
		signal.Notify(chSignal, syscall.SIGINT)
		<-chSignal
	}

	fmt.Println(`all ok`)
}

func simpleTest() {
	pool := h2client.NewConnectionPool(2)

	req := h2client.NewRequest()
	if err := req.ParseUrl(`https://ater.me/http2.txt`); err != nil {
		errors.Fprint(os.Stderr, err)
		return
	}
	fmt.Printf("req %#v\n", req)

	resp, err := pool.Do(req)
	if false {
		fmt.Println(err)
		fmt.Println(resp)
		fmt.Println(req)
	}
	if resp != nil {
		resp.Close()
	}

	return

	/*
		//http2conn, err := h2client.NewConnection(`http2.golang.org`, 443)
		http2conn, err := h2client.NewConnection(`ater.me`, 443)
		if err != nil {
			errors.Fprint(os.Stderr, err)
			return
		}
		defer http2conn.Close()

		//bodyRaw := []byte(`Hello, traveler! И немного кириллицы :)`+"\n"+strings.Repeat(`abc`, 100*1024))
		bodyRaw := []byte(`Hello, traveler! И немного кириллицы :)`)

		headers := []h2client.HeaderPair{
			h2client.HeaderPair{Key:`Foo`, Value:`Bar`},
			h2client.HeaderPair{Key:`X-Request-Info`, Value:`Hello, traveler!`},
			h2client.HeaderPair{Key:`Content-Length`, Value:strconv.Itoa(len(bodyRaw))},
		}

		for {
			atomic.AddUint32(&reqIdx, 1)
			if resp, err := http2conn.Req(`PUT`, `/http2.txt`, headers, bytes.NewBuffer(bodyRaw)); err != nil {
				errors.Fprint(os.Stderr, err)
				return
			} else {
				body, _ := ioutil.ReadAll(&resp.Body)
				//fmt.Printf("Status: %d\n\nHeaders: %v\n\nBody: [[[%s]]]\n", resp.Status, resp.Headers, string(body))
				if false {
					fmt.Printf("Status: %d\n\nHeaders: %v\n\nBody len: %d\n", resp.Status, resp.Headers, len(body))
				}
				resp.Close()
			}
		}
	*/
}

func stressTest() {
	/*
		//host := `http2.golang.org`
		//host := `new.vk.com`
		//host := `ater.me`
		//host := `api.vk.com`
		//host := `blog.cloudflare.com`

		for tcpConn := 1; tcpConn <= 8; tcpConn++ {
			http2conn, err := h2client.NewConnection(args.Host, 443)
			if err != nil {
				errors.Fprint(os.Stderr, err)
				return
			}
			//defer http2conn.Close()
			go func(http2conn *h2client.Connection) {

				pathes := []string{`/http2.txt`, `/http2_bower.json`}

				for th := 1; th <= 50; th++ {
					go func(th int) {
						for {
							curIdx := atomic.AddUint32(&reqIdx, 1)
							path := pathes[curIdx%uint32(len(pathes))]

							elapsed := time.Now().UnixNano()
							resp, err := http2conn.Req(`GET`, path, nil, nil)
							elapsed = (time.Now().UnixNano() - elapsed) / int64(time.Millisecond)

							if err != nil {
								errors.Fprint(os.Stderr, err)
								return
							} else {
								if false {
									fmt.Println(th, path, resp.Status, resp.Body.Len(), elapsed)
								}
								resp.Close()
							}
						}
					}(th)
				}
			}(http2conn)
			time.Sleep(1 * time.Second)
		}

		chSignal := make(chan os.Signal, 2)
		signal.Notify(chSignal, syscall.SIGINT)
		<-chSignal
	*/
}

func stressTest2() {
	/*
		bodyRaw := []byte(`Hello, traveler! И немного кириллицы :)`)

		headers := []h2client.HeaderPair{
			h2client.HeaderPair{Key:`Foo`, Value:`Bar`},
			h2client.HeaderPair{Key:`X-Request-Info`, Value:`Hello, traveler!`},
			h2client.HeaderPair{Key:`Content-Length`, Value:strconv.Itoa(len(bodyRaw))},
		}

		for tcpConn := 1; tcpConn <= 6; tcpConn++ {
			http2conn, err := h2client.NewConnection(`ater.me`, 443)
			if err != nil {
				errors.Fprint(os.Stderr, err)
				return
			}
			//defer http2conn.Close()
			go func(http2conn *h2client.Connection) {
				for th := 1; th <= 50; th++ {
					go func(th int) {
						for {
							atomic.AddUint32(&reqIdx, 1)

							elapsed := time.Now().UnixNano()
							resp, err := http2conn.Req(`PUT`, `/http2.txt`, headers, bytes.NewBuffer(bodyRaw))main
							elapsed = (time.Now().UnixNano() - elapsed) / int64(time.Millisecond)

							if err != nil {
								errors.Fprint(os.Stderr, err)
								return
							} else {
								if false {
									fmt.Println(th, `/http2.txt`, resp.Status, resp.Body.Len(), elapsed)
								}
								resp.Close()
							}
						}
					}(th)
				}
			}(http2conn)
			time.Sleep(2 * time.Second)
		}

		chSignal := make(chan os.Signal, 2)
		signal.Notify(chSignal, syscall.SIGINT)
		<-chSignal
	*/
}

func stressTest3() {
	pool := h2client.NewConnectionPool(20)

	req := h2client.NewRequest()
	if err := req.ParseUrl(`https://ater.me/http2.txt`); err != nil {
		errors.Fprint(os.Stderr, err)
		return
	}
	fmt.Printf("req %#v\n", req)

	for tcpConn := 1; tcpConn <= 3*2048; tcpConn++ {
		go func() {
			for {
				resp, err := pool.Do(req)

				atomic.AddUint32(&reqIdx, 1)

				if err != nil {
					errors.Fprint(os.Stderr, err)
					os.Stderr.WriteString("\n\n")
					panic(err)
				}
				if resp.Canceled {
					fmt.Println(resp)
				} else if resp.Status != 200 {
					//fmt.Println(err)
					fmt.Println(resp)
					//fmt.Println(req)
				}
				if resp != nil {
					resp.Close()
				}
			}
		}()

		if tcpConn%200 == 0 {
			time.Sleep(1 * time.Second)
		}
	}
}
