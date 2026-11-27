package tools_test

import "net"

func netListen(p string) (net.Listener, error) { return net.Listen("unix", p) }
