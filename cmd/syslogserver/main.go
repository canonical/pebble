package main

import (
	"log"
	"net"

	"github.com/leodido/go-syslog/v4"
	"github.com/leodido/go-syslog/v4/octetcounting"
	"github.com/leodido/go-syslog/v4/rfc5424"
	"github.com/sanity-io/litter"
)

func main() {
	tcpAddr, err := net.ResolveTCPAddr("tcp", ":1514")
	if err != nil {
		log.Fatalf("ResolveTCPAddr error: %v", err)
	}
	listener, err := net.ListenTCP("tcp", tcpAddr)
	if err != nil {
		log.Fatalf("ListenTCP error: %v", err)
	}

	log.Printf("Listening on TCP port 1514")
	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("Accept error: %v", err)
			continue
		}
		go func(conn net.Conn) {
			defer conn.Close()

			parser := octetcounting.NewParser(
				syslog.WithListener(func(result *syslog.Result) {
					if result.Error != nil {
						log.Printf("Parser error: %v", result.Error)
						return
					}
					switch msg := result.Message.(type) {
					case *rfc5424.SyslogMessage:
						litterConfig := litter.Options{
							FormatTime: true,
						}
						log.Printf("Received %s", litterConfig.Sdump(msg))
					default:
						log.Printf("Unknown message type: %T", result.Message)
					}
				}))
			parser.Parse(conn)
		}(conn)
	}
}
