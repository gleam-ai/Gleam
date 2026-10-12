package server

import (
	"io"
	"os"

	"gleam/internal/agent"
)

func RunStdio(a *agent.Agent) error {
	svc := NewService(a)
	conn := NewConn(os.Stdin, os.Stdout)
	svc.Bind(conn)
	return conn.Serve()
}

func Serve(a *agent.Agent, r io.Reader, w io.Writer) error {
	svc := NewService(a)
	conn := NewConn(r, w)
	svc.Bind(conn)
	return conn.Serve()
}
