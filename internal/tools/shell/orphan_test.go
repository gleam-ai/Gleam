package shell

import (
	"context"
	"runtime"
	"testing"
	"time"
)

func orphanHolder(seconds int) string {
	if runtime.GOOS == "windows" {
		return "start /b ping -n " + itoa(seconds+1) + " 127.0.0.1"
	}
	return "sleep " + itoa(seconds) + " & echo done"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func TestShell_OrphanChildDoesNotHangWait(t *testing.T) {
	const orphanLife = 30
	tool := New(60 * time.Second)

	start := time.Now()
	out, err := tool.Execute(context.Background(), map[string]any{
		"command": orphanHolder(orphanLife),
	})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("命令本身应正常返回（不该报错），实际：%v", err)
	}
	if elapsed > 20*time.Second {
		t.Fatalf("被孤儿进程拖住了：耗时 %v（说明 WaitDelay 没生效，会挂到包超时）", elapsed)
	}

	m, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("结果应为 map，实际 %T", out)
	}
	if m["output_incomplete"] != true {
		t.Errorf("应标记输出可能不完整，实际 %v（静默截断比报错更坏）", m["output_incomplete"])
	}
	if s, _ := m["error"].(string); s == "" {
		t.Error("应在 error 字段里说明原因，否则读的人不知道输出为什么少了")
	}
}

func TestShell_NormalCommandNotMarkedIncomplete(t *testing.T) {
	tool := New(10 * time.Second)
	out, err := tool.Execute(context.Background(), map[string]any{"command": echoCmd("ok")})
	if err != nil {
		t.Fatalf("不该报错：%v", err)
	}
	m := out.(map[string]any)
	if m["output_incomplete"] == true {
		t.Error("正常结束的命令不该被标记为输出不完整")
	}
	if s, _ := m["stdout"].(string); s == "" {
		t.Error("正常命令的输出应被完整读到")
	}
}
