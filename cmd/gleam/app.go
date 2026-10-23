package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"syscall"
	"time"

	"gleam/internal/desktop"
	"gleam/internal/systray"
	"gleam/internal/webui"
)

func cmdApp(args []string) error {
	fs := flag.NewFlagSet("app", flag.ContinueOnError)
	configPath := fs.String("config", "", "配置文件路径")
	workspace := fs.String("workspace", "", "工作区根目录")
	dataDir := fs.String("data-dir", "", "数据目录")
	mockLLM := fs.Bool("mock-llm", false, "使用 Mock 模型")
	mockScript := fs.String("mock-script", "", "Mock 响应脚本 JSON 路径")
	addr := fs.String("addr", "127.0.0.1:8787", "监听地址")
	noBrowser := fs.Bool("no-browser", false, "启动时不自动打开窗口（仅托盘）")
	daemonMode := fs.Bool("daemon", false, "无界面守护模式（不打开窗口和托盘）")
	flagArgs, _ := splitFlagArgs(args, fs)
	if err := fs.Parse(flagArgs); err != nil {
		return err
	}

	listenAddr := *addr
	if busy, gleam := probeAddr(*addr); busy {
		if gleam {
			msg := "已请求其弹出窗口"
			if *daemonMode {
				msg = "守护进程已在运行，本次启动直接退出"
			} else {
				_, _ = http.Post("http://"+*addr+"/api/show-window", "", nil)
			}
			fmt.Fprintf(os.Stderr, "[gleam] 已有实例运行于 http://%s，%s\n", *addr, msg)
			return nil
		}
		if *daemonMode {
			return fmt.Errorf("守护模式要求的地址 %s 被其他程序占用", *addr)
		}
		listenAddr = "127.0.0.1:0"
	}

	rt, err := buildRuntime(*configPath, *workspace, *dataDir, *mockLLM, *mockScript, nil)
	if err != nil {
		return err
	}
	defer rt.cleanup()
	bindSchedulerFire(rt)

	srv := webui.NewServer(rt.agent)
	inner := srv.Handler()
	lastSeen := time.Now()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastSeen = time.Now()
		inner.ServeHTTP(w, r)
	})
	httpServer := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second}

	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return err
	}
	srv.BindAddr = ln.Addr().String()
	url := "http://" + ln.Addr().String() + "/"
	announceToken(srv)
	fmt.Fprintf(os.Stderr, "[gleam] 桌面端已启动: %s（托盘常驻，关闭窗口不退出）\n", url)

	go func() {
		if err := httpServer.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintln(os.Stderr, "[gleam] HTTP 错误:", err)
		}
	}()

	if *daemonMode {
		fmt.Fprintf(os.Stderr, "[gleam] 守护进程已启动: %s\n", url)
		return serveDaemon(httpServer, rt.agent.DataDir(), url)
	}

	openWindow := func() { desktop.OpenWindow(url, nil) }
	srv.ShowWindowFunc = openWindow

	go func() {
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, os.Interrupt)
		<-ch
		desktop.CloseWindow()
		_ = httpServer.Close()
		systray.Quit()
	}()

	if !*noBrowser {
		openWindow()
	}

	go func() {
		t := time.NewTicker(5 * time.Second)
		defer t.Stop()
		for range t.C {
			running := rt.agent.RunningTasks()
			pending := srv.PendingApprovals()
			statusLine := "引擎空闲"
			if running > 0 {
				statusLine = fmt.Sprintf("运行中：%d 个任务", running)
			}
			if pending > 0 {
				statusLine += fmt.Sprintf(" · %d 项待批准", pending)
			}
			systray.SetTooltip("Gleam · 微光 — " + statusLine)
		}
	}()

	openInBrowser := func() { _ = desktop.OpenURL(url) }
	err = systray.Run(systray.Config{
		Tooltip:    "Gleam · 微光（本地智能体）",
		OnActivate: openWindow,
		MenuItems: []systray.MenuItem{
			{Title: "打开 Gleam", OnClick: openWindow},
			{Title: "在浏览器中打开", OnClick: openInBrowser},
			{Title: "", Disabled: true, SepAfter: true},
			{Title: "退出", OnClick: func() {
				desktop.CloseWindow()
				_ = httpServer.Close()
				systray.Quit()
			}},
		},
	})
	if errors.Is(err, systray.ErrNotImplemented) {
		if !*noBrowser {
			openBrowser(url)
		}
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, os.Interrupt)
		<-ch
		_ = httpServer.Close()
		return nil
	}
	_ = lastSeen
	return err
}

func serveDaemon(srv *http.Server, dataDir, url string) error {
	logDir := filepath.Join(dataDir, "logs")
	_ = os.MkdirAll(logDir, 0o755)
	logPath := filepath.Join(logDir, "daemon.log")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[gleam] 打不开守护日志 %s：%v\n", logPath, err)
	} else {
		defer f.Close()
		fmt.Fprintf(f, "%s daemon started: %s (pid %d)\n", time.Now().Format(time.RFC3339), url, os.Getpid())
	}

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	<-ch
	if f != nil {
		fmt.Fprintf(f, "%s daemon stopping\n", time.Now().Format(time.RFC3339))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return srv.Shutdown(ctx)
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch goruntime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	default:
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	}
	_ = cmd.Start()
}

func probeAddr(addr string) (busy, isGleam bool) {
	conn, err := net.DialTimeout("tcp", addr, 800*time.Millisecond)
	if err != nil {
		return false, false
	}
	_ = conn.Close()
	client := &http.Client{Timeout: 1500 * time.Millisecond}
	resp, err := client.Get("http://" + addr + "/api/info")
	if err != nil {
		return true, false
	}
	defer resp.Body.Close()
	if resp.Header.Get("X-Gleam-Server") == "gleam" {
		return true, true
	}
	buf := make([]byte, 512)
	n, _ := resp.Body.Read(buf)
	return true, strings.Contains(string(buf[:n]), `"name":"gleam"`)
}

func openAppWindow(url string) {
	cmd := launchAppCmd(url)
	if cmd == nil {
		return
	}
	go func() { _ = cmd.Wait() }()
}

func launchAppCmd(url string) *exec.Cmd {
	for _, p := range browserPathsFor(goruntime.GOOS) {
		if _, err := os.Stat(p); err != nil {
			continue
		}
		cmd := exec.Command(p,
			"--app="+url,
			"--window-size=1280,840",
			"--no-first-run",
			"--no-default-browser-check",
			"--disable-features=msWebOOUI,msPdfOOUI,msSmartScreenProtection",
		)
		cmd.SysProcAttr = procAttr()
		if err := cmd.Start(); err == nil {
			return cmd
		}
	}
	for _, parts := range browserFallbackCmds(goruntime.GOOS, url) {
		cmd := exec.Command(parts[0], parts[1:]...)
		cmd.SysProcAttr = procAttr()
		if err := cmd.Start(); err == nil {
			return cmd
		}
	}
	return nil
}

func browserFallbackCmds(goos, url string) [][]string {
	switch goos {
	case "darwin":
		var cmds [][]string
		for _, app := range []string{"Google Chrome", "Microsoft Edge", "Chromium"} {
			cmds = append(cmds, []string{"open", "-na", app, "--args", "--app=" + url, "--window-size=1440,900"})
		}
		return append(cmds, []string{"open", url})
	case "windows":
		return [][]string{{"rundll32", "url.dll,FileProtocolHandler", url}}
	case "linux":
		return [][]string{{"xdg-open", url}}
	default:
		return [][]string{{"open", url}}
	}
}

func browserPathsFor(goos string) []string {
	switch goos {
	case "darwin":
		paths := []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
		}
		if home := os.Getenv("HOME"); home != "" {
			paths = append(paths,
				home+"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
				home+"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge")
		}
		return paths
	case "linux":
		return []string{
			"/usr/bin/google-chrome",
			"/usr/bin/google-chrome-stable",
			"/usr/bin/microsoft-edge",
			"/usr/bin/microsoft-edge-stable",
			"/usr/bin/chromium",
			"/usr/bin/chromium-browser",
		}
	default:
		progFiles := os.Getenv("ProgramFiles")
		progFilesX86 := os.Getenv("ProgramFiles(x86)")
		localAppData := os.Getenv("LocalAppData")
		var paths []string
		for _, base := range []string{progFilesX86, progFiles} {
			if base != "" {
				paths = append(paths, base+`\Microsoft\Edge\Application\msedge.exe`)
			}
		}
		for _, base := range []string{localAppData, progFiles, progFilesX86} {
			if base != "" {
				paths = append(paths, base+`\Google\Chrome\Application\chrome.exe`)
			}
		}
		return paths
	}
}
