// Package rpc provides Unix socket IPC between the lanmon server and connect CLI.
package rpc

import (
	"fmt"
	"net"
	netrpc "net/rpc"
	"os"

	"github.com/rs/zerolog"

	"lanmon/internal/store"
)

// Service is the RPC service exposed by the server.
type Service struct {
	store *store.Store
	log   zerolog.Logger
}

// ListActiveHostsArgs is the request for ListActiveHosts.
type ListActiveHostsArgs struct{}

// ListActiveHostsReply is the response for ListActiveHosts.
type ListActiveHostsReply struct {
	Hosts []store.HostRecord
}

// MarkKeyPushedArgs is the request for MarkKeyPushed.
type MarkKeyPushedArgs struct {
	MAC string
}

// MarkKeyPushedReply is the response for MarkKeyPushed.
type MarkKeyPushedReply struct {
	Success bool
}

// ListActiveHosts returns all active host records.
func (s *Service) ListActiveHosts(args *ListActiveHostsArgs, reply *ListActiveHostsReply) error {
	hosts, err := s.store.GetActive()
	if err != nil {
		return fmt.Errorf("fetching active hosts: %w", err)
	}
	reply.Hosts = hosts
	return nil
}

// MarkKeyPushed marks the SSH key as pushed for the given MAC address.
func (s *Service) MarkKeyPushed(args *MarkKeyPushedArgs, reply *MarkKeyPushedReply) error {
	if err := s.store.MarkKeyPushed(args.MAC); err != nil {
		return fmt.Errorf("marking key pushed: %w", err)
	}
	reply.Success = true
	return nil
}

// StartServer starts the Unix socket RPC server.
func StartServer(socketPath string, db *store.Store, log zerolog.Logger) error {
	service := &Service{store: db, log: log}

	server := netrpc.NewServer()
	if err := server.Register(service); err != nil {
		return fmt.Errorf("registering RPC service: %w", err)
	}

	// Remove existing socket file if present
	os.Remove(socketPath)

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", socketPath, err)
	}

	// Set socket permissions
	if err := os.Chmod(socketPath, 0660); err != nil {
		log.Warn().Err(err).Msg("Failed to set socket permissions")
	}

	log.Info().Str("socket", socketPath).Msg("RPC server started")

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				log.Error().Err(err).Msg("RPC accept error")
				continue
			}
			go server.ServeConn(conn)
		}
	}()

	return nil
}

// Client is a client for the lanmon RPC service.
type Client struct {
	client *netrpc.Client
}

// NewClient dials the Unix socket and returns an RPC client.
func NewClient(socketPath string) (*Client, error) {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("connecting to RPC socket %s: %w", socketPath, err)
	}
	return &Client{client: netrpc.NewClient(conn)}, nil
}

// Close closes the RPC client connection.
func (c *Client) Close() error {
	return c.client.Close()
}

// ListActiveHosts fetches all active hosts from the server.
func (c *Client) ListActiveHosts() ([]store.HostRecord, error) {
	args := &ListActiveHostsArgs{}
	reply := &ListActiveHostsReply{}
	if err := c.client.Call("Service.ListActiveHosts", args, reply); err != nil {
		return nil, err
	}
	return reply.Hosts, nil
}

// MarkKeyPushed tells the server to mark a host's SSH key as pushed.
func (c *Client) MarkKeyPushed(mac string) error {
	args := &MarkKeyPushedArgs{MAC: mac}
	reply := &MarkKeyPushedReply{}
	return c.client.Call("Service.MarkKeyPushed", args, reply)
}
