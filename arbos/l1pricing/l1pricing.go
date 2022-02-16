//
// Copyright 2021, Offchain Labs, Inc. All rights reserved.
//

package l1pricing

import (
	"github.com/offchainlabs/arbstate/arbos/addressSet"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/params"
	"github.com/offchainlabs/arbstate/arbos/storage"
	"github.com/offchainlabs/arbstate/arbos/util"
)

type L1PricingState struct {
	storage                     *storage.Storage
	defaultAggregator           storage.StorageBackedAddress
	l1GasPriceEstimate          storage.StorageBackedBigInt
	userSpecifiedAggregators    *storage.Storage
	refuseDefaultAggregator     *storage.Storage
	aggregatorFixedCharges      *storage.Storage
	aggregatorFeeCollectors     *storage.Storage
	aggregatorCompressionRatios *storage.Storage
}

var (
	SequencerAddress = common.HexToAddress("0xA4B000000000000000000073657175656e636572")

	userSpecifiedAggregatorKey    = []byte{0}
	refuseDefaultAggregatorKey    = []byte{1}
	aggregatorFixedChargeKey      = []byte{2}
	aggregatorFeeCollectorKey     = []byte{3}
	aggregatorCompressionRatioKey = []byte{4}
)

const (
	defaultAggregatorAddressOffset uint64 = 0
	l1GasPriceEstimateOffset       uint64 = 1
)

func InitializeL1PricingState(sto *storage.Storage) error {
	err := sto.SetByUint64(defaultAggregatorAddressOffset, common.BytesToHash(SequencerAddress.Bytes()))
	if err != nil {
		return err
	}
	return sto.SetByUint64(l1GasPriceEstimateOffset, common.BigToHash(big.NewInt(50*params.GWei)))
}

func OpenL1PricingState(sto *storage.Storage) *L1PricingState {
	return &L1PricingState{
		sto,
		sto.OpenStorageBackedAddress(defaultAggregatorAddressOffset),
		sto.OpenStorageBackedBigInt(l1GasPriceEstimateOffset),
		sto.OpenSubStorage(userSpecifiedAggregatorKey),
		sto.OpenSubStorage(refuseDefaultAggregatorKey),
		sto.OpenSubStorage(aggregatorFixedChargeKey),
		sto.OpenSubStorage(aggregatorFeeCollectorKey),
		sto.OpenSubStorage(aggregatorCompressionRatioKey),
	}
}

func (ps *L1PricingState) DefaultAggregator() (common.Address, error) {
	return ps.defaultAggregator.Get()
}

func (ps *L1PricingState) SetDefaultAggregator(val common.Address) error {
	return ps.defaultAggregator.Set(val)
}

func (ps *L1PricingState) L1GasPriceEstimateWei() (*big.Int, error) {
	return ps.l1GasPriceEstimate.Get()
}

func (ps *L1PricingState) SetL1GasPriceEstimateWei(val *big.Int) error {
	return ps.l1GasPriceEstimate.Set(val)
}

const L1GasPriceEstimateMemoryWeight = 24

func (ps *L1PricingState) UpdateL1GasPriceEstimate(baseFeeWei *big.Int) error {
	curr, err := ps.L1GasPriceEstimateWei()
	if err != nil {
		return err
	}

	// new = (alpha * old + observed) / (alpha + 1)
	memory := new(big.Int).Mul(curr, big.NewInt(L1GasPriceEstimateMemoryWeight))
	impact := new(big.Int).Add(memory, baseFeeWei)
	update := new(big.Int).Div(impact, big.NewInt(L1GasPriceEstimateMemoryWeight+1))

	return ps.SetL1GasPriceEstimateWei(update)
}

func (ps *L1PricingState) userSpecifiedAggregatorsForAddress(sender common.Address) *addressSet.AddressSet {
	return addressSet.OpenAddressSet(ps.userSpecifiedAggregators.OpenSubStorage(sender.Bytes()))
}

// Get sender's user-specified aggregator, or nil if there is none. This does NOT fall back to the default aggregator
//     if there is no user-specified aggregator. If that is what you want, call ReimbursableAggregatorForSender instead.
func (ps *L1PricingState) UserSpecifiedAggregator(sender common.Address) (*common.Address, error) {
	return ps.userSpecifiedAggregatorsForAddress(sender).GetAnyMember()
}

func (ps *L1PricingState) SetUserSpecifiedAggregator(sender common.Address, maybeAggregator *common.Address) error {
	paSet := ps.userSpecifiedAggregatorsForAddress(sender)
	if err := paSet.Clear(); err != nil {
		return err
	}
	if maybeAggregator == nil {
		return nil
	}
	return paSet.Add(*maybeAggregator)
}

func (ps *L1PricingState) RefusesDefaultAggregator(addr common.Address) (bool, error) {
	val, err := ps.refuseDefaultAggregator.Get(common.BytesToHash(addr.Bytes()))
	if err != nil {
		return false, err
	}
	return val != (common.Hash{}), nil
}

func (ps *L1PricingState) SetRefusesDefaultAggregator(addr common.Address, refuses bool) error {
	val := uint64(0)
	if refuses {
		val = 1
	}
	return ps.refuseDefaultAggregator.Set(common.BytesToHash(addr.Bytes()), common.BigToHash(new(big.Int).SetUint64(val)))
}

// Get the aggregator who is eligible to be reimbursed for L1 costs of txs from sender, or nil if there is none.
func (ps *L1PricingState) ReimbursableAggregatorForSender(sender common.Address) (*common.Address, error) {
	fromTable, err := ps.UserSpecifiedAggregator(sender)
	if err != nil {
		return nil, err
	}
	if fromTable != nil {
		return fromTable, nil
	}

	refuses, err := ps.RefusesDefaultAggregator(sender)
	if err != nil || refuses {
		return nil, err
	}
	aggregator, err := ps.DefaultAggregator()
	if err != nil {
		return nil, err
	}
	if aggregator == (common.Address{}) {
		return nil, nil
	}
	return &aggregator, nil

}

func (ps *L1PricingState) SetFixedChargeForAggregatorL1Gas(aggregator common.Address, chargeL1Gas *big.Int) error {
	return ps.aggregatorFixedCharges.Set(common.BytesToHash(aggregator.Bytes()), common.BigToHash(chargeL1Gas))
}

func (ps *L1PricingState) FixedChargeForAggregatorL1Gas(aggregator common.Address) (*big.Int, error) {
	value, err := ps.aggregatorFixedCharges.Get(common.BytesToHash(aggregator.Bytes()))
	return value.Big(), err
}
func (ps *L1PricingState) FixedChargeForAggregatorWei(aggregator common.Address) (*big.Int, error) {
	fixed, err := ps.FixedChargeForAggregatorL1Gas(aggregator)
	if err != nil {
		return nil, err
	}
	price, err := ps.L1GasPriceEstimateWei()
	if err != nil {
		return nil, err
	}
	return new(big.Int).Mul(fixed, price), nil
}

func (ps *L1PricingState) SetAggregatorFeeCollector(aggregator common.Address, addr common.Address) error {
	return ps.aggregatorFeeCollectors.Set(common.BytesToHash(aggregator.Bytes()), common.BytesToHash(addr.Bytes()))
}

func (ps *L1PricingState) AggregatorFeeCollector(aggregator common.Address) (common.Address, error) {
	raw, err := ps.aggregatorFeeCollectors.Get(common.BytesToHash(aggregator.Bytes()))
	if raw == (common.Hash{}) {
		return aggregator, err
	} else {
		return common.BytesToAddress(raw.Bytes()), err
	}
}

func (ps *L1PricingState) AggregatorCompressionRatio(aggregator common.Address) (uint64, error) {
	raw, err := ps.aggregatorCompressionRatios.Get(common.BytesToHash(aggregator.Bytes()))
	if raw == (common.Hash{}) {
		return DataWasNotCompressed, err
	} else {
		return raw.Big().Uint64(), err
	}
}

func (ps *L1PricingState) SetAggregatorCompressionRatio(aggregator common.Address, ratio uint64) error {
	val := DataWasNotCompressed
	if ratio < DataWasNotCompressed {
		val = ratio
	}
	return ps.aggregatorCompressionRatios.Set(util.AddressToHash(aggregator), util.UintToHash(val))
}

// Compression ratio is expressed in fixed-point representation.  A value of DataWasNotCompressed corresponds to
//    a compression ratio of 1, that is, no compression.
// A value of x (for x <= DataWasNotCompressed) corresponds to compression ratio of float(x) / float(DataWasNotCompressed).
// Values greater than DataWasNotCompressed are treated as equivalent to DataWasNotCompressed.

const DataWasNotCompressed uint64 = 1000000
const TxFixedCost = 100 // assumed size in bytes of a typical RLP-encoded tx, not including its calldata

func (ps *L1PricingState) PosterDataCost(
	sender common.Address,
	aggregator *common.Address,
	data []byte,
) (*big.Int, error) {
	if aggregator == nil {
		return big.NewInt(0), nil
	}
	reimbursableAggregator, err := ps.ReimbursableAggregatorForSender(sender)
	if err != nil {
		return nil, err
	}
	if reimbursableAggregator == nil {
		return big.NewInt(0), nil
	}
	if *reimbursableAggregator != *aggregator {
		return big.NewInt(0), nil
	}

	bytesToCharge := uint64(len(data) + TxFixedCost)

	ratio, err := ps.AggregatorCompressionRatio(*reimbursableAggregator)
	if err != nil {
		return nil, err
	}

	dataGas := 16 * bytesToCharge * ratio / DataWasNotCompressed

	// add 5% to protect the aggregator from bad price fluctuation luck
	dataGas = dataGas * 21 / 20

	price, err := ps.L1GasPriceEstimateWei()
	if err != nil {
		return nil, err
	}

	baseCharge, err := ps.FixedChargeForAggregatorWei(*reimbursableAggregator)
	if err != nil {
		return nil, err
	}

	chargeForBytes := new(big.Int).Mul(big.NewInt(int64(dataGas)), price)
	return new(big.Int).Add(baseCharge, chargeForBytes), nil
}
