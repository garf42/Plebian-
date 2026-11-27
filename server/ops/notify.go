package ops

import (
	"net"
	"os"
	"time"
)

// Notify sends one sd_notify message ("READY=1", "WATCHDOG=1") when systemd asked for them.
func Notify(state string) {
	sock := os.Getenv("NOTIFY_SOCKET")
	if sock == "" {
		return
	}
	if sock[0] == '@' {
		sock = "\x00" + sock[1:]
	}
	c, err := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: sock, Net: "unixgram"})
	if err != nil {
		return
	}
	defer c.Close()
	c.Write([]byte(state))
}

// Heartbeat beats the watchdog at half its interval while alive() holds.
func Heartbeat(alive func() bool, stop <-chan struct{}) {
	t := time.NewTicker(WatchdogSec * time.Second / 2)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			if alive() {
				Notify("WATCHDOG=1")
			}
		}
	}
}
