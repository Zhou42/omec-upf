// SPDX-License-Identifier: Apache-2.0
// Copyright 2020 Intel Corporation

package pfcpiface

import (
	"net"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
)

type EndMarker struct {
	TEID     uint32
	PeerIP   net.IP
	PeerPort uint16
}

// CreateFAR appends far to existing list of FARs in the session.
func (s *PFCPSession) CreateFAR(f Far) {
	s.Fars = append(s.Fars, f)
}

func addEndMarkerForGtp(farItem Far, endMarkerList *[]EndMarker) {
	newEndMarker := EndMarker{
		TEID:     farItem.TunnelTEID,
		PeerIP:   int2ip(farItem.TunnelIP4Dst),
		PeerPort: farItem.TunnelPort,
	}
	*endMarkerList = append(*endMarkerList, newEndMarker)
}

func addEndMarker(farItem Far, endMarkerList *[][]byte) {
	// This time lets fill out some information
	log.Info("Adding end Marker for farID : ", farItem.FarID)

	options := gopacket.SerializeOptions{
		ComputeChecksums: true,
		FixLengths:       true,
	}
	buffer := gopacket.NewSerializeBuffer()
	ipLayer := &layers.IPv4{
		Version:  4,
		TTL:      64,
		SrcIP:    int2ip(farItem.TunnelIP4Src),
		DstIP:    int2ip(farItem.TunnelIP4Dst),
		Protocol: layers.IPProtocolUDP,
	}
	ethernetLayer := &layers.Ethernet{
		SrcMAC:       net.HardwareAddr{0xFF, 0xAA, 0xFA, 0xAA, 0xFF, 0xAA},
		DstMAC:       net.HardwareAddr{0xBD, 0xBD, 0xBD, 0xBD, 0xBD, 0xBD},
		EthernetType: layers.EthernetTypeIPv4,
	}
	udpLayer := &layers.UDP{
		SrcPort: layers.UDPPort(2152),
		DstPort: layers.UDPPort(2152),
	}

	err := udpLayer.SetNetworkLayerForChecksum(ipLayer)
	if err != nil {
		log.Warn("set checksum for UDP layer in endmarker failed")
		return
	}

	gtpLayer := &layers.GTPv1U{
		Version:      1,
		MessageType:  254,
		ProtocolType: farItem.TunnelType,
		TEID:         farItem.TunnelTEID,
	}
	// And create the packet with the layers
	err = gopacket.SerializeLayers(buffer, options,
		ethernetLayer,
		ipLayer,
		udpLayer,
		gtpLayer,
	)

	if err == nil {
		outgoingPacket := buffer.Bytes()
		*endMarkerList = append(*endMarkerList, outgoingPacket)
	} else {
		log.Warn("go packet serialize failed : ", err)
	}
}

// UpdateFAR updates existing far in the session.
func (s *PFCPSession) UpdateFAR(f *Far, endMarkerList *[]EndMarker) error {
	for idx, v := range s.Fars {
		if v.FarID == f.FarID {
			if f.SendEndMarker {
				addEndMarkerForGtp(v, endMarkerList)
			}

			s.Fars[idx] = *f

			return nil
		}
	}

	return ErrNotFound("FAR")
}

// RemoveFAR removes far from existing list of FARs in the session.
func (s *PFCPSession) RemoveFAR(id uint32) (*Far, error) {
	for idx, v := range s.Fars {
		if v.FarID == id {
			s.Fars = append(s.Fars[:idx], s.Fars[idx+1:]...)
			return &v, nil
		}
	}

	return nil, ErrNotFound("FAR")
}
