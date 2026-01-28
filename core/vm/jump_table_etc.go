// Copyright 2024 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package vm

var (
	mystiqueInstructionSet JumpTable
	spiralInstructionSet   JumpTable
)

func init() {
	mystiqueInstructionSet = newMystiqueInstructionSet()
	spiralInstructionSet = newSpiralInstructionSet()
}

// newMystiqueInstructionSet returns the ETC Mystique (ECIP-1104) instruction set.
// Mystique = Berlin + EIP-3529 (gas refund reduction).
// Unlike ETH London, Mystique does NOT include EIP-3198 (BASEFEE opcode)
// or EIP-1559 (fee market).
func newMystiqueInstructionSet() JumpTable {
	instructionSet := newBerlinInstructionSet()
	enable3529(&instructionSet) // EIP-3529: Reduction in refunds
	return validate(instructionSet)
}

// newSpiralInstructionSet returns the ETC Spiral instruction set.
// Spiral = Mystique + EIP-3855 (PUSH0) + EIP-3860 (initcode size limit).
func newSpiralInstructionSet() JumpTable {
	instructionSet := newMystiqueInstructionSet()
	enable3855(&instructionSet) // PUSH0
	enable3860(&instructionSet) // initcode limit
	return validate(instructionSet)
}
