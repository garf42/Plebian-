// Command fixture starts one fixture and prints one JSON line {"addr": ..., ...} once
// it is listening, then serves until killed.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"plebian/tests/fixtures"
	"plebian/tests/fixtures/clock"
	"plebian/tests/fixtures/ical"
	"plebian/tests/fixtures/mail"
	"plebian/tests/fixtures/model"
	"plebian/tests/fixtures/omi"
	"plebian/tests/fixtures/s3"
	"plebian/tests/fixtures/speech"
	"plebian/tests/fixtures/texts"
	"plebian/tests/fixtures/web"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: fixture model|time|mail|ical|texts|web|s3|speech|omi [-addr 127.0.0.1:0]")
		os.Exit(2)
	}
	kind := os.Args[1]
	fs := flag.NewFlagSet(kind, flag.ExitOnError)
	addr := fs.String("addr", "127.0.0.1:0", "listen address")
	key := fs.String("key", "", "credential the fixture accepts (random when empty)")
	user := fs.String("user", "owner", "account name (mail, web)")
	imapAddr := fs.String("imap", "127.0.0.1:0", "IMAP listen address (mail)")
	smtpAddr := fs.String("smtp", "127.0.0.1:0", "SMTP listen address (mail)")
	useTLS := fs.Bool("tls", false, "implicit TLS on the mail listeners")
	dev := omi.DefaultConfig()
	fs.StringVar(&dev.Firmware, "firmware", dev.Firmware, "DIS firmware revision; picks the storage protocol (omi)")
	codecID := fs.Int("codec", dev.Codec.ID, "Opus codec id, 21 or 20 (omi)")
	fs.StringVar(&dev.Serial, "serial", dev.Serial, "DIS serial number (omi)")
	fs.IntVar(&dev.Battery, "battery", dev.Battery, "battery percent (omi)")
	packetStart := fs.Uint("packet-start", 0, "audio packet counter at boot (omi)")
	fs.Parse(os.Args[2:])
	if *key == "" {
		*key = fixtures.Secret(16)
	}
	var srv *fixtures.Server
	var err error
	serve := func(h http.Handler) { srv, err = fixtures.ServeHTTP(*addr, h) }
	switch kind {
	case "model":
		serve(model.New(*key).Handler())
	case "time":
		serve(clock.New().Handler())
	case "ical":
		serve(ical.New().Handler())
	case "texts":
		serve(texts.New().Handler())
	case "web":
		serve(web.New(*user, *key).Handler())
	case "s3":
		b := s3.New(*user, *key)
		serve(b.Handler())
	case "speech":
		serve(speech.Handler())
	case "mail":
		srv, err = mail.New(*user, *key).Start(*addr, *imapAddr, *smtpAddr, *useTLS)

	// fixture omi: the Omi simulator on loopback, standing in for BLE behind the
	// app's device seam (the app's integration tests on an Android emulator reach
	// it at 10.0.2.2). Characteristics are lowercase UUIDs (omi/gatt.go); every
	// byte value is base64 in JSON. One phone at a time.
	//
	//   GET  /phone/events      WebSocket. Opening it connects the phone, closing it
	//                           disconnects. Server text frames, one Event each:
	//                           {"op":"connected"} | {"op":"disconnected"} |
	//                           {"op":"notify","char":"<uuid>","value":"<base64>"}
	//   POST /phone/read        {"char"} -> {"value"}
	//   POST /phone/write       {"char","value"} -> {"ok":true}
	//   POST /phone/subscribe   {"char"} -> {"ok":true}   (the CCC write)
	//   POST /phone/mtu         {"mtu"} -> {"mtu": negotiated}, min(asked, 498)
	//   A refused phone call answers 409 {"error":"<ATT error>"}.
	//
	//   POST /control/talk       {"lines":[{"speaker":"a|b","text","pause_ms"}]} -> {"frames","duration_ms","segments"};
	//                            plays in real time, live while connected, subscribed and MTU >= 100, else into storage
	//   POST /control/double_tap -> {"heard"}: notified only while connected and subscribed
	//   POST /control/range      {"in": false} drops the link (disconnected), {"in": true} restores it (connected)
	//   POST /control/clock      {"device_unix_ms"} sets the device clock (skew)
	//   POST /control/battery    {"level"}
	//   GET  /control/writes     every phone write: {"time","char","value","note","error"}; note decodes it ("haptic 100 ms")
	//   GET  /control/storage    what the device holds offline, in its protocol's layout
	//   GET  /control/state      {"connected","in_range","mtu","subscribed","packet_next","firmware","codec","protocol","talking","clock_set"}
	case "omi":
		if dev.Codec, err = speech.CodecByID(*codecID); err == nil && *packetStart <= 0xFFFF {
			dev.PacketStart = uint16(*packetStart)
			dev.FrameInterval = time.Duration(dev.Codec.FrameMs()) * time.Millisecond
			var d *omi.Device
			if d, err = omi.New(dev); err == nil {
				serve(omi.Handler(d))
			}
		} else if err == nil {
			err = fmt.Errorf("packet-start %d: the packet number is a u16", *packetStart)
		}
	default:
		fmt.Fprintln(os.Stderr, "unknown fixture", kind)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if _, ok := srv.Info["key"]; !ok {
		srv.Info["key"] = *key
	}
	if kind == "s3" || kind == "web" {
		srv.Info["user"] = *user
	}
	b, _ := json.Marshal(srv.Info)
	fmt.Println(string(b))
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	<-sig
	srv.Close()
}
