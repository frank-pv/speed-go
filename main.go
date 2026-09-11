package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// 模拟最新版 Chrome (Windows x64) 的 User-Agent
const defaultChromeUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/152.0.0.0 Safari/537.36"

const defaultChromeHeadersTemplate = `Accept: text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7
Accept-Language: zh-CN,zh;q=0.9,en;q=0.8
Sec-Ch-Ua: "Chromium";v="152", "Not_A Brand";v="24"
Sec-Ch-Ua-Mobile: ?0
Sec-Ch-Ua-Platform: "Windows"
Sec-Fetch-Dest: document
Sec-Fetch-Mode: navigate
Upgrade-Insecure-Requests: 1
`

const usageText = `speed-go: HTTP/HTTPS 下载压测工具

用法:
  speed-go [参数] -url <URL>

示例:
  speed-go -url https://example.com/file.zip
  speed-go -c 10 -t 120s -k -ua "Mozilla/5.0 ..." -url https://example.com/file.zip
  speed-go -referer "https://example.com/page.html" -url https://example.com/file.zip

参数:
  -url string     压测目标 URL (必填)
  -c int          并发连接数 (默认 5)
  -t duration     压测时长, 如 60 / 60s / 2m (默认 60s)
  -k              忽略 HTTPS 证书校验
  -ua string      自定义 User-Agent (默认模拟最新版 Chrome)
  -referer string 请求的来源页面 Referer (默认不携带)
  -H string       附加额外请求头, 格式 "Key: Value", 可多次使用
  -h, -help       显示本帮助

说明:
  - 行为尽可能贴近 Chrome: 携带 Chrome 默认请求头、优先使用 HTTP/2、gzip/br/zstd 等编码协商
  - 每秒实时打印当前速率与累计流量, 下载过程中按 Read 返回的数据块实时计数
  - Ctrl+C 可随时中断, 中断或结束后均会输出压测结果统计
`

type config struct {
	url          string
	connections  int
	duration     string
	insecure     bool
	userAgent    string
	referer      string
	extraHeaders map[string]string
}

func main() {
	cfg := config{extraHeaders: map[string]string{}}

	var extraHeaders headerList
	showHelp := false

	flag.StringVar(&cfg.url, "url", "", "压测目标 URL")
	flag.IntVar(&cfg.connections, "c", 5, "并发连接数")
	flag.StringVar(&cfg.duration, "t", "60s", "压测时长")
	flag.BoolVar(&cfg.insecure, "k", false, "忽略 HTTPS 证书校验")
	flag.StringVar(&cfg.userAgent, "ua", defaultChromeUA, "自定义 User-Agent")
	flag.StringVar(&cfg.referer, "referer", "", "来源页面 Referer")
	flag.Var(&extraHeaders, "H", `附加请求头 "Key: Value", 可多次使用`)
	flag.BoolVar(&showHelp, "h", false, "显示帮助")
	flag.BoolVar(&showHelp, "help", false, "显示帮助")
	flag.Usage = func() { fmt.Fprint(os.Stderr, usageText) }
	flag.Parse()

	// 兼容旧的写法: 未指定 -url 时, 取第一个位置参数
	if cfg.url == "" && flag.NArg() > 0 {
		cfg.url = flag.Arg(0)
	}
	if showHelp || cfg.url == "" {
		fmt.Fprint(os.Stderr, usageText)
		os.Exit(2)
	}
	for k, v := range extraHeaders {
		cfg.extraHeaders[k] = v
	}

	// Ctrl+C 优雅退出
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, parseDuration(cfg.duration))
	defer cancel()

	run(ctx, &cfg)
}

// headerList 支持多次 -H "Key: Value"
type headerList map[string]string

func (h *headerList) String() string { return "" }

func (h *headerList) Set(v string) error {
	idx := strings.Index(v, ":")
	if idx <= 0 {
		return fmt.Errorf("请求头格式应为 \"Key: Value\": %q", v)
	}
	if *h == nil {
		*h = headerList{}
	}
	(*h)[strings.TrimSpace(v[:idx])] = strings.TrimSpace(v[idx+1:])
	return nil
}

type stats struct {
	bytes    atomic.Int64 // 累计下载字节数(按 Read 实时累加)
	requests atomic.Int64 // 完成的请求总数
	errors   atomic.Int64 // 出错请求数
}

// parseDuration 兼容 "60"(秒) / "60s" / "2m" 两种写法
func parseDuration(s string) time.Duration {
	if d, err := time.ParseDuration(s); err == nil {
		return d
	}
	// 纯数字按秒处理
	if n := strings.TrimSpace(s); n != "" && isAllDigits(n) {
		return time.Duration(mustAtoi(n)) * time.Second
	}
	fmt.Fprintf(os.Stderr, "无效的时长参数: %q (支持 60 / 60s / 2m)\n", s)
	os.Exit(2)
	return 0
}

func isAllDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func mustAtoi(s string) int {
	n := 0
	for _, r := range s {
		n = n*10 + int(r-'0')
	}
	return n
}

func run(ctx context.Context, cfg *config) {
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		ForceAttemptHTTP2:     true, // 贴近 Chrome: https 优先走 h2
		MaxConnsPerHost:       cfg.connections,
		MaxIdleConns:          cfg.connections,
		MaxIdleConnsPerHost:   cfg.connections,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: cfg.insecure,
		},
	}
	defer transport.CloseIdleConnections()

	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("重定向次数过多")
			}
			// 重定向时保持 UA 等自定义头
			req.Header.Set("User-Agent", cfg.userAgent)
			return nil
		},
	}

	st := &stats{}
	start := time.Now()
	fmt.Printf("开始压测: %s\n  连接数: %d | 时长: %s | 忽略证书: %v\n  UA: %s\n",
		cfg.url, cfg.connections, cfg.duration, cfg.insecure, cfg.userAgent)
	if cfg.referer != "" {
		fmt.Printf("  Referer: %s\n", cfg.referer)
	}

	var wg sync.WaitGroup
	for i := 0; i < cfg.connections; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			worker(ctx, client, cfg, st)
		}()
	}

	// 每秒打印速率与累计流量
	reportDone := make(chan struct{})
	go func() {
		defer close(reportDone)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		var lastBytes int64
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				total := st.bytes.Load()
				rate := float64(total-lastBytes) / (1024 * 1024)
				lastBytes = total
				fmt.Printf("[%s] 速率: %8.2f MB/s | 总流量: %s | 请求数: %d(错误%d)\n",
					time.Now().Format("15:04:05"), rate, humanBytes(total),
					st.requests.Load(), st.errors.Load())
			}
		}
	}()

	wg.Wait()
	<-reportDone

	printSummary(start, st, ctx.Err() == context.Canceled)
}

// worker 单个连接循环发请求, 直到超时或被取消
func worker(ctx context.Context, client *http.Client, cfg *config, st *stats) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		if download(ctx, client, cfg, st) {
			st.requests.Add(1)
		} else {
			st.errors.Add(1)
			// 出错后小睡片刻, 避免死循环打满 CPU
			select {
			case <-ctx.Done():
				return
			case <-time.After(200 * time.Millisecond):
			}
		}
	}
}

// download 执行一次完整下载, 边读边累加字节数; 返回是否成功
func download(ctx context.Context, client *http.Client, cfg *config, st *stats) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.url, nil)
	if err != nil {
		return false
	}

	// 模拟 Chrome 的默认请求头
	for _, line := range strings.Split(defaultChromeHeadersTemplate, "\n") {
		if idx := strings.Index(line, ":"); idx > 0 {
			req.Header.Set(strings.TrimSpace(line[:idx]), strings.TrimSpace(line[idx+1:]))
		}
	}
	req.Header.Set("User-Agent", cfg.userAgent)
	if cfg.referer != "" {
		req.Header.Set("Referer", cfg.referer)
		req.Header.Set("Sec-Fetch-Site", "cross-site")
	} else {
		req.Header.Set("Sec-Fetch-Site", "none")
	}
	// 显式声明 Accept-Encoding 后, Go 不会做透明解压, 统计的是真实线上字节数
	req.Header.Set("Accept-Encoding", "gzip, deflate, br, zstd")
	for k, v := range cfg.extraHeaders {
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		io.Copy(io.Discard, resp.Body)
		return false
	}

	buf := make([]byte, 64*1024)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			// 每次返回的数据块实时累加, 不等下载完成
			st.bytes.Add(int64(n))
		}
		if err != nil {
			if err == io.EOF {
				return true
			}
			// 被取消属于正常结束
			select {
			case <-ctx.Done():
				return true
			default:
				return false
			}
		}
	}
}

func printSummary(start time.Time, st *stats, interrupted bool) {
	elapsed := time.Since(start)
	total := st.bytes.Load()
	avg := float64(total) / elapsed.Seconds()

	fmt.Println("\n================ 压测结果 ================")
	if interrupted {
		fmt.Println("  (用户手动中断)")
	}
	fmt.Printf("  压测时长:   %s\n", elapsed.Round(time.Millisecond))
	fmt.Printf("  总流量:     %s (%d bytes)\n", humanBytes(total), total)
	fmt.Printf("  平均速率:   %.2f MB/s\n", avg/(1024*1024))
	fmt.Printf("  完成请求:   %d\n", st.requests.Load())
	fmt.Printf("  失败请求:   %d\n", st.errors.Load())
	fmt.Println("==========================================")
}

func humanBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
