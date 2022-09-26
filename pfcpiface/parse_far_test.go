// SPDX-License-Identifier: Apache-2.0
// Copyright 2022-present Open Networking Foundation

package pfcpiface

import (
	"net"
	"testing"

	pfcpsimLib "github.com/omec-project/pfcpsim/pkg/pfcpsim/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wmnsk/go-pfcp/ie"
)

type farTestCase struct {
	input       *ie.IE
	op          operation
	expected    *Far
	description string
}

const (
	defaultGTPProtocolPort = 2152
)

func TestParseFAR(t *testing.T) {
	createOp, updateOp := create, update

	var FSEID uint64 = 100

	coreIP := net.ParseIP("10.0.10.1")
	UEAddressForDownlink := net.ParseIP("10.0.1.1")

	for _, scenario := range []farTestCase{
		{
			op: createOp,
			input: pfcpsimLib.NewFARBuilder().
				WithID(999).
				WithMethod(pfcpsimLib.Create).
				WithAction(ActionDrop).
				WithDstInterface(core).
				BuildFAR(),
			expected: &Far{
				FarID:       999,
				ApplyAction: ActionDrop,
				FseID:       FSEID,
			},
			description: "Valid Uplink FAR input with create operation",
		},
		{
			op: updateOp,
			input: pfcpsimLib.NewFARBuilder().
				WithID(1).
				WithAction(ActionForward).
				WithMethod(pfcpsimLib.Update).
				WithDstInterface(access).
				WithDownlinkIP(UEAddressForDownlink.String()).
				WithTEID(100).
				BuildFAR(),
			expected: &Far{
				FarID:        1,
				FseID:        FSEID,
				ApplyAction:  ActionForward,
				DstIntf:      access,
				TunnelTEID:   100,
				TunnelType:   access,
				TunnelIP4Src: ip2int(coreIP),
				TunnelIP4Dst: ip2int(UEAddressForDownlink),
				TunnelPort:   uint16(defaultGTPProtocolPort),
			},
			description: "Valid Downlink FAR input with update operation",
		},
	} {
		t.Run(scenario.description, func(t *testing.T) {
			mockFar := &Far{}
			mockUpf := &Upf{
				AccessIP: net.ParseIP("192.168.0.1"),
				CoreIP:   coreIP,
			}

			err := mockFar.parseFAR(scenario.input, FSEID, mockUpf, scenario.op)
			require.NoError(t, err)

			assert.Equal(t, scenario.expected, mockFar)
		})
	}
}

func TestParseFARShouldError(t *testing.T) {
	createOp, updateOp := create, update

	var FSEID uint64 = 101

	for _, scenario := range []farTestCase{
		{
			op: createOp,
			input: ie.NewCreateFAR(
				ie.NewFARID(1),
				ie.NewApplyAction(0),
				ie.NewForwardingParameters(
					ie.NewDestinationInterface(ie.DstInterfaceCore),
				),
			),
			expected: &Far{
				FarID: 1,
				FseID: FSEID,
			},
			description: "Uplink FAR with invalid action",
		},
		{
			op: updateOp,
			input: ie.NewUpdateFAR(
				ie.NewFARID(1),
				ie.NewApplyAction(0),
				ie.NewUpdateForwardingParameters(
					ie.NewDestinationInterface(ie.DstInterfaceAccess),
					ie.NewOuterHeaderCreation(0x100, 100, "10.0.0.1", "", 0, 0, 0),
				),
			),
			expected: &Far{
				FarID: 1,
				FseID: FSEID,
			},
			description: "Downlink FAR with invalid action",
		},
		{
			op: createOp,
			input: ie.NewCreateFAR(
				ie.NewApplyAction(ActionDrop),
				ie.NewUpdateForwardingParameters(
					ie.NewDestinationInterface(ie.DstInterfaceAccess),
					ie.NewOuterHeaderCreation(0x100, 100, "10.0.0.1", "", 0, 0, 0),
				),
			),
			expected: &Far{
				FseID: FSEID,
			},
			description: "Malformed Downlink FAR with missing FARID",
		},
	} {
		t.Run(scenario.description, func(t *testing.T) {
			mockFar := &Far{}
			mockUpf := &Upf{
				AccessIP: net.ParseIP("192.168.0.1"),
				CoreIP:   net.ParseIP("10.0.0.1"),
			}

			err := mockFar.parseFAR(scenario.input, 101, mockUpf, scenario.op)
			require.Error(t, err)

			assert.Equal(t, scenario.expected, mockFar)
		})
	}
}
