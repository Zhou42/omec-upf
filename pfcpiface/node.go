// SPDX-License-Identifier: Apache-2.0
// Copyright 2021 Intel Corporation
// Copyright 2021 Open Networking Foundation
package pfcpiface

import (
	"context"
	"errors"
	"net"
	"sync"

	reuse "github.com/libp2p/go-reuseport"
	"go.uber.org/zap"

	"github.com/omec-project/upf-epc/pfcpiface/metrics"
)

// PFCPNode represents a PFCP endpoint of the UPF.
type PFCPNode struct {
	ctx    context.Context
	cancel context.CancelFunc
	// listening socket for new "PFCP connections"
	net.PacketConn
	// done is closed to signal shutdown complete
	done chan struct{}
	// channel for PFCPConn to signal exit by sending their remote address
	pConnDone chan string
	// map of existing connections
	pConns sync.Map
	// upf
	upf *Upf
	// metrics for PFCP messages and sessions
	metrics metrics.InstrumentPFCP
}

// NewPFCPNode create a new PFCPNode listening on local address.
func NewPFCPNode(upf *Upf) *PFCPNode {
	conn, err := reuse.ListenPacket("udp", ":"+PFCPPort)
	if err != nil {
		log.Fatal("ListenUDP failed ", err)
	}

	metrics, err := metrics.NewPrometheusService()
	if err != nil {
		log.Fatal("prom metrics service init failed ", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &PFCPNode{
		ctx:        ctx,
		cancel:     cancel,
		PacketConn: conn,
		done:       make(chan struct{}),
		pConnDone:  make(chan string, 100),
		upf:        upf,
		metrics:    metrics,
	}
}

func (node *PFCPNode) tryConnectToN4Peers(lAddrStr string) {
	for _, peer := range node.upf.peers {
		conn, err := net.Dial("udp", peer+":"+PFCPPort)
		if err != nil {
			log.Warn("Failed to establish PFCP connection to peer ", peer)
			continue
		}

		remoteAddr := conn.RemoteAddr().(*net.UDPAddr)
		n4DstIP := remoteAddr.IP

		log.Infow(
			"Establishing PFCP Conn with CP node",
			zap.String("SPGWC/SMF host", peer),
			zap.String("CP node", n4DstIP.String()),
		)

		pfcpConn := node.NewPFCPConn(lAddrStr, n4DstIP.String()+":"+PFCPPort, nil)
		if pfcpConn != nil {
			go pfcpConn.sendAssociationRequest()
		}
	}
}

func (node *PFCPNode) handleNewPeers() {
	lAddrStr := node.LocalAddr().String()
	log.Info("listening for new PFCP connections on ", lAddrStr)

	node.tryConnectToN4Peers(lAddrStr)

	for {
		buf := make([]byte, 1024)

		n, rAddr, err := node.ReadFrom(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}

			continue
		}

		rAddrStr := rAddr.String()

		_, ok := node.pConns.Load(rAddrStr)
		if ok {
			log.Warn("Drop packet for existing PFCPconn received from ", rAddrStr)
			continue
		}

		node.NewPFCPConn(lAddrStr, rAddrStr, buf[:n])
	}
}

// Serve listens for the first packet from a new PFCP peer and creates PFCPConn.
func (node *PFCPNode) Serve() {
	go node.handleNewPeers()

	shutdown := false

	for !shutdown {
		select {
		case fseid := <-node.upf.ReportNotifyChan:
			// TODO: Logic to distinguish PFCPConn based on SEID
			node.pConns.Range(func(key, value interface{}) bool {
				pConn := value.(*PFCPConn)
				pConn.handleDigestReport(fseid)
				return false
			})
		case rAddr := <-node.pConnDone:
			node.pConns.Delete(rAddr)
			log.Info("Removed connection to ", rAddr)
		case <-node.ctx.Done():
			shutdown = true

			log.Info("Shutting down PFCP node")

			err := node.Close()
			if err != nil {
				log.Error("Error closing PFCPNode conn ", err)
			}

			// Clear out the remaining pconn completions
		clearLoop:
			for {
				select {
				case rAddr, ok := <-node.pConnDone:
					{
						if !ok {
							// channel is closed, break
							break clearLoop
						}
						node.pConns.Delete(rAddr)
						log.Info("Removed connection to ", rAddr)
					}
				default:
					// nothing to read from channel
					break clearLoop
				}
			}

			if len(node.pConnDone) > 0 {
				for rAddr := range node.pConnDone {
					node.pConns.Delete(rAddr)
					log.Info("Removed connection to ", rAddr)
				}
			}

			close(node.pConnDone)
			log.Info("Done waiting for PFCPConn completions")

			node.upf.Exit()
		}
	}

	close(node.done)
}

func (node *PFCPNode) Stop() {
	node.cancel()

	if err := node.metrics.Stop(); err != nil {
		// TODO: propagate error upwards
		log.Error(err)
	}
}

// Done waits for Shutdown() to complete
func (node *PFCPNode) Done() {
	<-node.done
	log.Info("Shutdown complete")
}
