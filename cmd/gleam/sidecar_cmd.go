package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"gleam/internal/webui"
)

const sidecarReadyPrefix = "GLEAM_READY "

const sidecarShutdownGrace = 3 * time.Second

type sidecarReady struct {
	Addr    string `json:"addr"`
	PID     int    `json:"pid"`
	Version string `json:"version"`
}

func cmdDesktopSidecar(args []string) error {
	fs := flag.NewFlagSet("desktop-sidecar", flag.ContinueOnError)
	configPath := fs.String("config", "", "配置文件路径")
	workspace := fs.String("workspace", "", "工作区根目录")
	dataDir := fs.String("data-dir", "", "数据目录")
	mockLLM := fs.Bool("mock-llm", false, "使用 Mock 模型")
	mockScript := fs.String("mock-script", "", "Mock 响应脚本 JSON 路径")
	addr := fs.String("addr", "127.0.0.1:0", "监听地址（必须是回环地址）")
	flagArgs, _ := splitFlagArgs(args, fs)
	if err := fs.Parse(flagArgs); err != nil {
		return err
	}
	if !isLoopbackListenAddr(*addr) {
		return fmt.Errorf("desktop-sidecar 只允许监听回环地址，收到 %q", *addr)
	}
	if strings.TrimSpace(os.Getenv(webui.TokenEnv)) == "" {
		fmt.Fprintf(os.Stderr, "[gleam] 警告：未设置 %s，desktop-sidecar 将使用随机口令（桌面壳拿不到它）\n", webui.TokenEnv)
	}

	rt, err := buildRuntime(*configPath, *workspace, *dataDir, *mockLLM, *mockScript, nil, WithNoDefaultWorkspace(), WithAsyncMCP())
	if err != nil {
		return err
	}
	defer rt.cleanup()
	bindSchedulerFire(rt)

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		return err
	}
	srv := webui.NewServer(rt.agent)
	_ = os.Unsetenv(webui.TokenEnv)
	srv.BindAddr = ln.Addr().String()
	announceToken(srv)
	go rt.startMCP()
	return runSidecar(srv.Handler(), ln, os.Stdin, os.Stdout)
}

func isLoopbackListenAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func runSidecar(h http.Handler, ln net.Listener, stdin io.Reader, stdout io.Writer) error {
	baseCtx, cancelBase := context.WithCancel(context.Background())
	defer cancelBase()
	httpServer := &http.Server{
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return baseCtx },
	}

	serveErr := make(chan error, 1)
	go func() { serveErr <- httpServer.Serve(ln) }()

	ready, _ := json.Marshal(sidecarReady{Addr: ln.Addr().String(), PID: os.Getpid(), Version: version})
	if _, err := fmt.Fprintf(stdout, "%s%s\n", sidecarReadyPrefix, ready); err != nil {
		cancelBase()
		_ = httpServer.Close()
		return fmt.Errorf("写就绪行失败: %w", err)
	}

	stop := make(chan string, 1)
	var once sync.Once
	signalStop := func(why string) { once.Do(func() { stop <- why }) }
	go func() {
		sc := bufio.NewScanner(stdin)
		for sc.Scan() {
			var msg struct {
				Cmd string `json:"cmd"`
			}
			if json.Unmarshal(sc.Bytes(), &msg) == nil && msg.Cmd == "shutdown" {
				signalStop("shutdown 指令")
				return
			}
		}
		signalStop("stdin 已关闭")
	}()

	select {
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case why := <-stop:
		fmt.Fprintf(os.Stderr, "[gleam] desktop-sidecar 收到退出信号（%s），正在关闭…\n", why)
	}
	cancelBase()
	ctx, cancel := context.WithTimeout(context.Background(), sidecarShutdownGrace)
	defer cancel()
	if err := httpServer.Shutdown(ctx); err != nil {
		_ = httpServer.Close()
	}
	return nil
}
