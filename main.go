package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"sync"

	"github.com/creack/pty"
	"github.com/mdlayher/vsock"
)

const (
	FrameTypeData       = 0x01
	FrameTypeWindowSize = 0x02
)

func main() {

	fmt.Printf(`slicer-ssh-agent - Copyright (c) 2025 Alex Ellis, OpenFaaS Ltd
`)

	port := uint32(514)
	l, err := vsock.Listen(port, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer l.Close()

	for {
		conn, err := l.Accept()
		if err != nil {
			log.Fatal(err)
		}

		fmt.Printf("Connection accepted from socket\n")

		go handleConnection(conn)
	}
}

func handleConnection(conn net.Conn) {
	defer conn.Close()

	var err error
	// Try multiple shells in order of preference
	shells := []string{"/bin/bash", "/bin/sh", "/usr/bin/sh"}
	var ptty *os.File
	var shellCmd *exec.Cmd

	for _, shell := range shells {
		log.Printf("Trying to start shell: %s", shell)
		shellCmd = exec.Command(shell, "--login") // Add -l flag for login shell

		// Set environment variables to make it look like SSH
		shellCmd.Env = append(os.Environ(),
			"SSH_TTY=/dev/pts/0",
			"TERM=xterm-256color")

		if _, err := os.Stat("/etc/update-motd.d/"); err == nil {
			shellCmd.Args = append(shellCmd.Args, "-c", "/usr/bin/run-parts /etc/update-motd.d/; exec bash")
		}
		log.Printf("Starting shell: %v", shellCmd.Args)

		// Don't set Setpgid as it might be causing permission issues
		ptty, err = pty.Start(shellCmd)
		if err == nil {
			log.Printf("Successfully started shell: %s", shell)
			break
		}
		log.Printf("Failed to start shell %s: %v", shell, err)
	}

	if err != nil {
		log.Printf("Could not start any shell: %v", err)
		sendErrorMessage(conn, fmt.Sprintf("Failed to start any command: %v", err))
		return
	}

	defer ptty.Close()

	// Set initial window size
	var ws pty.Winsize
	ws.Cols = 80
	ws.Rows = 24
	pty.Setsize(ptty, &ws)

	// Create a mutex to protect writes to the connection
	var connMutex sync.Mutex

	// Function to send a framed message
	sendFrame := func(frameType byte, payload []byte) error {
		connMutex.Lock()
		defer connMutex.Unlock()

		// Write frame header
		header := make([]byte, 5)
		header[0] = frameType
		binary.BigEndian.PutUint32(header[1:5], uint32(len(payload)))

		if _, err := conn.Write(header); err != nil {
			return err
		}

		// Write payload
		if len(payload) > 0 {
			if _, err := conn.Write(payload); err != nil {
				return err
			}
		}

		return nil
	}

	// Create a WaitGroup to track active goroutines
	var wg sync.WaitGroup
	wg.Add(2)

	// Create a channel to signal process exit
	processDone := make(chan struct{})

	// Start a goroutine to wait for the process to exit
	go func() {
		shellCmd.Wait()
		close(processDone)
		conn.Close()

		log.Printf("Shell process exited")
	}()

	// Start a goroutine to read from the PTY and send to the connection
	go func() {
		defer wg.Done()
		buf := make([]byte, 32*1024)
		for {
			select {
			case <-processDone:
				//close the connection

				log.Printf("Process exited, stopping PTY reader")
				return
			default:
				n, err := ptty.Read(buf)
				if err != nil {
					if err != io.EOF {
						log.Printf("Error reading from PTY: %v", err)
					}
					return
				}

				// Send data frame
				if err := sendFrame(FrameTypeData, buf[:n]); err != nil {
					log.Printf("Error sending data frame: %v", err)
					return
				}
			}
		}
	}()

	// Start a goroutine to read framed messages from the connection
	go func() {
		defer wg.Done()

		frameHeader := make([]byte, 5)
		for {
			select {
			case <-processDone:
				log.Printf("Process exited, stopping connection reader")
				return
			default:
				// Read frame header
				if _, err := io.ReadFull(conn, frameHeader); err != nil {
					if err != io.EOF {
						log.Printf("Error reading frame header: %v", err)
					}
					// Kill the process if the connection is closed
					if shellCmd.Process != nil {
						shellCmd.Process.Kill()
					}
					return
				}

				frameType := frameHeader[0]
				payloadLen := binary.BigEndian.Uint32(frameHeader[1:5])

				// Read payload
				payload := make([]byte, payloadLen)
				if payloadLen > 0 {
					if _, err := io.ReadFull(conn, payload); err != nil {
						log.Printf("Error reading frame payload: %v", err)
						// Kill the process if the connection is closed
						if shellCmd.Process != nil {
							shellCmd.Process.Kill()
						}
						return
					}
				}

				// Process frame
				switch frameType {
				case FrameTypeData:
					// Write data to PTY
					if _, err := ptty.Write(payload); err != nil {
						log.Printf("Error writing to PTY: %v", err)
						return
					}
				case FrameTypeWindowSize:
					// Resize PTY window
					if len(payload) >= 8 {
						width := binary.BigEndian.Uint32(payload[0:4])
						height := binary.BigEndian.Uint32(payload[4:8])

						var ws pty.Winsize
						ws.Cols = uint16(width)
						ws.Rows = uint16(height)
						pty.Setsize(ptty, &ws)
						log.Printf("Resized PTY to %dx%d", width, height)
					}
				default:
					log.Printf("Unknown frame type: %d", frameType)
				}
			}
		}
	}()

	// Wait for all goroutines to complete
	wg.Wait()

	// Ensure the process is terminated
	if shellCmd.Process != nil {
		shellCmd.Process.Kill()
	}

	log.Printf("Connection handler completed")
}

func sendErrorMessage(conn net.Conn, message string) {
	log.Printf("Sending error message: %s", message)
	header := make([]byte, 5)
	header[0] = FrameTypeData
	binary.BigEndian.PutUint32(header[1:5], uint32(len(message)))
	conn.Write(header)
	conn.Write([]byte(message))
}
