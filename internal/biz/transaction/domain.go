package transaction

import (
	"database/sql"
	"strconv"

	v1 "cobo-ucw-backend/api/ucw/v1"
	"cobo-ucw-backend/internal/data/model"

	CoboWaas2 "github.com/CoboGlobal/cobo-waas2-go-sdk/cobo_waas2"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/shopspring/decimal"
)

type Transaction struct {
	*model.Transaction
}

func BuildTransactionFromWebhook(req *v1.CoboTransaction, logger *log.Helper) (*Transaction, error) {

	amount, err := decimal.NewFromString(req.GetDestination().GetAmount())
	if err != nil {
		logger.Warnf("invalid amount txid %s", req.GetTransactionId())
		amount = decimal.Zero
	}

	feeUsed, err := decimal.NewFromString(req.GetFee().GetFeeUsed())
	if err != nil {
		logger.Warnf("empty feeUsed txid %s", req.GetTransactionId())
		feeUsed = decimal.Zero
	}

	gasLimit, err := decimal.NewFromString(req.GetFee().GetGasLimit())
	if err != nil {
		logger.Warnf("empty gasLimit txid %s", req.GetTransactionId())
		gasLimit = decimal.Zero
	}

	gasPrice, err := decimal.NewFromString(req.GetFee().GetEffectiveGasPrice())
	if err != nil {
		logger.Warnf("empty gasPrice txid %s", req.GetTransactionId())
		gasPrice = decimal.Zero
	}

	maxPriorityFee, err := decimal.NewFromString(req.GetFee().GetMaxPriorityFeePerGas())
	if err != nil {
		logger.Warnf("empty gasPrice txid %s", req.GetTransactionId())
		maxPriorityFee = decimal.Zero
	}

	maxFeePerGas, err := decimal.NewFromString(req.GetFee().GetMaxFeePerGas())
	if err != nil {
		logger.Warnf("empty maxFeePerGas txid %s", req.GetTransactionId())
		maxFeePerGas = decimal.Zero
	}
	feeRate, err := decimal.NewFromString(req.GetFee().GetFeeRate())
	if err != nil {
		logger.Warnf("empty feeRate txid %s", req.GetTransactionId())
		feeRate = decimal.Zero
	}
	feeAmount, err := decimal.NewFromString(req.GetFee().GetFeeAmount())
	if err != nil {
		logger.Warnf("empty feeAmount txid %s", req.GetTransactionId())
		feeAmount = decimal.Zero
	}
	var from = ""
	if CoboWaas2.TransactionSourceType(req.GetSource().GetSourceType()) == CoboWaas2.TRANSACTIONSOURCETYPE_DEPOSIT_FROM_ADDRESS {
		if len(req.GetSource().GetAddresses()) > 0 {
			from = req.GetSource().GetAddresses()[0]
		}
	}

	tx := &Transaction{
		Transaction: &model.Transaction{
			TransactionID: "",
			WalletID:      req.GetWalletId(),
			Type:          int64(v1.Transaction_DEPOSIT),
			Chain:         req.GetChainId(),
			Amount:        amount,
			From:          from,
			To:            req.GetDestination().GetAddress(),
			TxHash:        req.GetTransactionHash(),
			Fee: model.Fee{
				TokenID:        req.GetFee().GetTokenId(),
				GasPrice:       gasPrice,
				GasLimit:       gasLimit,
				FeePerByte:     feeRate,
				FeeAmount:      feeAmount,
				Level:          0,
				MaxPriorityFee: maxPriorityFee,
				MaxFee:         maxFeePerGas,
				FeeUsed:        feeUsed,
			},
			Status: int64(statusSet[CoboWaas2.TransactionStatus(req.GetStatus())]),
			ExternalID: sql.NullString{
				String: req.GetTransactionId(),
				Valid:  true,
			},
			TokenID:  req.GetTokenId(),
			BlockNum: req.GetBlockInfo().GetBlockNumber(),
			Extra: model.Extra{
				FailedReason:        req.GetFailedReason(),
				ConfirmedNum:        req.GetConfirmedNum(),
				ConfirmingThreshold: req.GetConfirmingThreshold(),
				BlockHash:           req.GetBlockInfo().GetBlockHash(),
				Description:         req.GetDescription(),
				Nonce:               req.GetRawTxInfo().GetUsedNonce(),
				RawTx:               req.GetRawTxInfo().GetRawTx(),
			},
		},
	}
	return tx, nil
}

func (t *Transaction) GetFee() *Fee {
	f := Fee(t.Fee)
	return &f
}

func (t *Transaction) ToProto() *v1.Transaction {
	if t == nil || t.Transaction == nil {
		return nil
	}
	return &v1.Transaction{
		TransactionId: t.TransactionID,
		Type:          v1.Transaction_Type(t.Type),
		Chain:         t.Chain,
		Amount: &v1.Amount{
			Value: t.Amount.String(),
			Token: &v1.Token{
				TokenId: t.TokenID,
				Name:    "",
				Decimal: 0,
				Symbol:  "",
				Chain:   "",
				IconUrl: "",
			},
		},
		From:            t.From,
		To:              t.To,
		CreateTimestamp: strconv.FormatInt(t.CreatedAt.Unix(), 10),
		TxHash:          t.TxHash,
		Fee:             t.GetFee().ToProto(),
		Status:          v1.Transaction_Status(t.Status),
		WalletId:        t.WalletID,
		SubStatus:       v1.Transaction_SubStatus(t.SubStatus),
		ExternalId:      t.ExternalID.String,
	}
}

type Fee model.Fee

func (f *Fee) ToEvmEip1559TransactionFee() *CoboWaas2.TransactionRequestEvmEip1559Fee {
	if f.MaxPriorityFee.IsZero() {
		return nil
	}
	return &CoboWaas2.TransactionRequestEvmEip1559Fee{
		MaxFeePerGas:         f.MaxFee.String(),
		MaxPriorityFeePerGas: f.MaxPriorityFee.String(),
		FeeType:              CoboWaas2.FEETYPE_EVM_EIP_1559,
		TokenId:              f.TokenID,
		GasLimit:             CoboWaas2.PtrString(f.GasLimit.String()),
	}
}

func (f *Fee) ToEvmLegacyTransactionFee() *CoboWaas2.TransactionRequestEvmLegacyFee {
	if f.GasPrice.IsZero() {
		return nil
	}
	return &CoboWaas2.TransactionRequestEvmLegacyFee{
		GasPrice: f.GasPrice.String(),
		GasLimit: CoboWaas2.PtrString(f.GasLimit.String()),
		FeeType:  CoboWaas2.FEETYPE_EVM_LEGACY,
		TokenId:  f.TokenID,
	}
}

func (f *Fee) ToUtxoTransactionFee() *CoboWaas2.TransactionRequestUtxoFee {
	if f.FeePerByte.IsZero() {
		return nil
	}
	return &CoboWaas2.TransactionRequestUtxoFee{
		FeeRate:      CoboWaas2.PtrString(f.FeePerByte.String()),
		MaxFeeAmount: CoboWaas2.PtrString(f.MaxFee.String()),
		FeeType:      CoboWaas2.FEETYPE_UTXO,
		TokenId:      f.TokenID,
	}
}

type EstimatedFee struct {
	Fast      *Fee
	Recommend *Fee
	Slow      *Fee
}

func (f *Fee) ToProto() *v1.Fee {
	if f == nil {
		return nil
	}
	return &v1.Fee{
		FeePerByte:     f.FeePerByte.String(),
		GasPrice:       f.GasPrice.String(),
		GasLimit:       f.GasLimit.String(),
		Level:          v1.Fee_Level(f.Level),
		MaxFee:         f.MaxFee.String(),
		MaxPriorityFee: f.MaxPriorityFee.String(),
		TokenId:        f.TokenID,
		FeeAmount:      f.FeeAmount.String(),
	}
}

func (f *EstimatedFee) Apply(applyF func(f *EstimatedFee) error) error {
	return applyF(f)
}

func BuildEstimatedFeeFromPortal(src *CoboWaas2.EstimatedFee) (*EstimatedFee, error) {
	fee := &EstimatedFee{
		Fast: &Fee{
			Level: int64(v1.Fee_FAST),
		},
		Recommend: &Fee{
			Level: int64(v1.Fee_RECOMMEND),
		},
		Slow: &Fee{
			Level: int64(v1.Fee_SLOW),
		},
	}
	if err := fee.Apply(func(fee *EstimatedFee) error {
		switch v := src.GetActualInstance().(type) {
		case *CoboWaas2.EstimatedEvmEip1559Fee:
			fee.Fast.MaxFee = decimal.RequireFromString(v.GetFast().MaxFeePerGas)
			fee.Fast.MaxPriorityFee = decimal.RequireFromString(v.GetFast().MaxPriorityFeePerGas)
			fee.Fast.GasLimit = decimal.RequireFromString(v.GetFast().GasLimit)
			fee.Recommend.MaxFee = decimal.RequireFromString(v.GetRecommended().MaxFeePerGas)
			fee.Recommend.MaxPriorityFee = decimal.RequireFromString(v.GetRecommended().MaxPriorityFeePerGas)
			fee.Recommend.GasLimit = decimal.RequireFromString(v.GetRecommended().GasLimit)
			fee.Slow.MaxFee = decimal.RequireFromString(v.GetSlow().MaxFeePerGas)
			fee.Slow.MaxPriorityFee = decimal.RequireFromString(v.GetSlow().MaxPriorityFeePerGas)
			fee.Slow.GasLimit = decimal.RequireFromString(v.GetSlow().GasLimit)
		case *CoboWaas2.EstimatedEvmLegacyFee:
			fee.Fast.GasPrice = decimal.RequireFromString(v.GetFast().GasPrice)
			fee.Fast.GasLimit = decimal.RequireFromString(v.GetFast().GasLimit)
			fee.Recommend.GasPrice = decimal.RequireFromString(v.GetRecommended().GasPrice)
			fee.Recommend.GasLimit = decimal.RequireFromString(v.GetRecommended().GasLimit)
			fee.Slow.GasPrice = decimal.RequireFromString(v.GetSlow().GasPrice)
			fee.Slow.GasLimit = decimal.RequireFromString(v.GetSlow().GasLimit)
		case *CoboWaas2.EstimatedUtxoFee:
			fee.Fast.FeePerByte = decimal.RequireFromString(v.GetFast().FeeRate)
			fee.Fast.FeeAmount = decimal.RequireFromString(v.GetFast().FeeAmount)
			fee.Recommend.FeePerByte = decimal.RequireFromString(v.GetRecommended().FeeRate)
			fee.Recommend.FeeAmount = decimal.RequireFromString(v.GetRecommended().FeeAmount)
			fee.Slow.FeePerByte = decimal.RequireFromString(v.GetSlow().FeeRate)
			fee.Slow.FeeAmount = decimal.RequireFromString(v.GetSlow().FeeAmount)
		case *CoboWaas2.EstimatedFixedFee:
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return fee, nil
}

func (t *Transaction) ApplyFeeUsed(src *CoboWaas2.Transaction) error {
	f := t.GetFee()

	if err := f.Apply(func(fee *Fee) error {
		var err error
		switch v := src.Fee.GetActualInstance().(type) {
		case *CoboWaas2.TransactionEvmEip1559Fee:
			if v.GetFeeUsed() == "" {
				return nil
			}
			fee.FeeUsed, err = decimal.NewFromString(v.GetFeeUsed())
			if err != nil {
				return err
			}
			return nil
		case *CoboWaas2.TransactionEvmLegacyFee:
			if v.GetFeeUsed() == "" {
				return nil
			}
			fee.FeeUsed, err = decimal.NewFromString(v.GetFeeUsed())
			if err != nil {
				return err
			}
		default:
		}
		return nil
	}); err != nil {
		return err
	}

	t.Fee = model.Fee(*f)
	return nil
}

func (t *Transaction) ApplyExtra(src *CoboWaas2.Transaction) {
	t.Extra.RawTx = src.RawTxInfo.GetRawTx()
	t.Extra.BlockHash = src.BlockInfo.GetBlockHash()
	t.BlockNum = src.BlockInfo.GetBlockNumber()
	t.Extra.Description = src.GetDescription()
	t.Extra.FailedReason = src.GetFailedReason()
	t.Extra.ConfirmedNum = int64(src.GetConfirmedNum())
	t.Extra.ConfirmingThreshold = int64(src.GetConfirmingThreshold())
	t.Extra.Nonce = int64(src.RawTxInfo.GetUsedNonce())
}

func (t *Transaction) ApplyStatus(src *CoboWaas2.Transaction) bool {
	status := statusSet[src.GetStatus()]
	if int64(status) < t.Status {
		return false
	}
	t.ApplySubStatus(src)
	t.Status = int64(status)
	return true
}

func (t *Transaction) ApplySubStatus(src *CoboWaas2.Transaction) bool {
	if src.GetSubStatus() == CoboWaas2.TRANSACTIONSUBSTATUS_PENDING_APPROVAL_START {
		t.SubStatus = int64(v1.Transaction_SUB_STATUS_PENDING_SIGNATURE_CAN_BE_APPROVED)
		return true
	}
	return false
}

func (t *Transaction) ApplyTxHash(src *CoboWaas2.Transaction) {
	if src.GetTransactionHash() == "" {
		return
	}
	t.TxHash = src.GetTransactionHash()
}

func BuildFeeFromProto(fee *v1.Fee) (*Fee, error) {
	f := &Fee{
		TokenID:        fee.GetTokenId(),
		GasPrice:       decimal.Decimal{},
		GasLimit:       decimal.Decimal{},
		FeePerByte:     decimal.Decimal{},
		FeeAmount:      decimal.Decimal{},
		Level:          int64(fee.Level),
		MaxPriorityFee: decimal.Decimal{},
		MaxFee:         decimal.Decimal{},
	}
	var err error
	if fee.MaxPriorityFee != "" {
		f.MaxPriorityFee, err = decimal.NewFromString(fee.MaxPriorityFee)
		if err != nil {
			return nil, err
		}
	}
	if fee.MaxFee != "" {
		f.MaxFee, err = decimal.NewFromString(fee.MaxFee)
		if err != nil {
			return nil, err
		}
	}
	if fee.GasPrice != "" {
		f.GasPrice, err = decimal.NewFromString(fee.GasPrice)
		if err != nil {
			return nil, err
		}
	}
	if fee.GasLimit != "" {
		f.GasLimit, err = decimal.NewFromString(fee.GasLimit)
		if err != nil {
			return nil, err
		}
	}
	if fee.FeePerByte != "" {
		f.FeePerByte, err = decimal.NewFromString(fee.FeePerByte)
		if err != nil {
			return nil, err
		}
	}
	if fee.FeeAmount != "" {
		f.FeeAmount, err = decimal.NewFromString(fee.FeeAmount)
		if err != nil {
			return nil, err
		}
	}

	return f, err
}

func (t *Transaction) Apply(f func(transaction *Transaction) error) error {
	return f(t)
}

func (f *Fee) Apply(applyF func(fee *Fee) error) error {
	return applyF(f)
}
