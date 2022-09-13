// SPDX-License-Identifier: Apache-2.0
// Copyright 2020 Intel Corporation

package pfcpiface

import (
	"fmt"

	"github.com/wmnsk/go-pfcp/ie"
)

type operation int

const (
	FwdIEOuterHeaderCreation Bits = 1 << iota
	FwdIEDestinationIntf
	FwdIEPfcpSMReqFlags
)

const (
	ActionForward = 0x2
	ActionDrop    = 0x1
	ActionBuffer  = 0x4
	ActionNotify  = 0x8
)

const (
	create operation = iota
	update
)

type Far struct {
	FarID   uint32
	FseID   uint64
	FseidIP uint32

	DstIntf       uint8
	SendEndMarker bool
	ApplyAction   uint8
	TunnelType    uint8
	TunnelIP4Src  uint32
	TunnelIP4Dst  uint32
	TunnelTEID    uint32
	TunnelPort    uint16
}

func (f Far) String() string {
	return fmt.Sprintf("FAR(id=%v, F-SEID=%v, F-SEID IPv4=%v, dstInterface=%v, tunnelType=%v, "+
		"tunnelIPv4Src=%v, tunnelIPv4Dst=%v, tunnelTEID=%v, tunnelSrcPort=%v, "+
		"sendEndMarker=%v, drops=%v, forwards=%v, buffers=%v)", f.FarID, f.FseID, int2ip(f.FseidIP), f.DstIntf,
		f.TunnelType, int2ip(f.TunnelIP4Src), int2ip(f.TunnelIP4Dst), f.TunnelTEID, f.TunnelPort, f.SendEndMarker,
		f.Drops(), f.Forwards(), f.Buffers())
}

func (f *Far) Drops() bool {
	return f.ApplyAction&ActionDrop != 0
}

func (f *Far) Buffers() bool {
	return f.ApplyAction&ActionBuffer != 0
}

func (f *Far) Forwards() bool {
	return f.ApplyAction&ActionForward != 0
}

func (f *Far) parseFAR(farIE *ie.IE, fseid uint64, upf *Upf, op operation) error {
	f.FseID = (fseid)

	farID, err := farIE.FARID()
	if err != nil {
		return err
	}

	f.FarID = farID

	action, err := farIE.ApplyAction()
	if err != nil {
		return err
	}

	if action == 0 {
		return ErrInvalidArgument("FAR Action", action)
	}

	f.ApplyAction = action

	var fwdIEs []*ie.IE

	switch op {
	case create:
		if (f.ApplyAction & ActionForward) != 0 {
			fwdIEs, err = farIE.ForwardingParameters()
		}
	case update:
		fwdIEs, err = farIE.UpdateForwardingParameters()
	default:
		return ErrInvalidOperation(op)
	}

	if err != nil {
		return err
	}

	f.SendEndMarker = false

	var fields Bits

	for _, fwdIE := range fwdIEs {
		switch fwdIE.Type {
		case ie.OuterHeaderCreation:
			fields = Set(fields, FwdIEOuterHeaderCreation)

			ohcFields, ohcErr := fwdIE.OuterHeaderCreation()
			if ohcErr != nil {
				log.Warnf("Unable to parse OuterHeaderCreationFields! %v", err)
				continue
			}

			f.TunnelTEID = ohcFields.TEID
			f.TunnelIP4Dst = ip2int(ohcFields.IPv4Address)
			f.TunnelType = uint8(1) // FIXME: what does it mean?
			f.TunnelPort = tunnelGTPUPort
		case ie.DestinationInterface:
			fields = Set(fields, FwdIEDestinationIntf)

			f.DstIntf, err = fwdIE.DestinationInterface()
			if err != nil {
				log.Warnf("Unable to parse DestinationInterface field %v", err)
				continue
			}

			switch f.DstIntf {
			case ie.DstInterfaceAccess:
				f.TunnelIP4Src = ip2int(upf.AccessIP)
			case ie.DstInterfaceCore:
				f.TunnelIP4Src = ip2int(upf.CoreIP)
			}

		case ie.PFCPSMReqFlags:
			fields = Set(fields, FwdIEPfcpSMReqFlags)

			smReqFlags, err := fwdIE.PFCPSMReqFlags()
			if err != nil {
				log.Warnf("Unable to parse PFCPSMReqFlags! %v", err)
				continue
			}

			if has2ndBit(smReqFlags) {
				f.SendEndMarker = true
			}
		}
	}

	return nil
}
