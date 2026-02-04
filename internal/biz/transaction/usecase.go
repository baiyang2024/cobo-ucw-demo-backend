package transaction

import (
	"context"
	"database/sql"
	"strings"

	v1 "cobo-ucw-backend/api/ucw/v1"
	"cobo-ucw-backend/integration/portal"
	"cobo-ucw-backend/internal/conf"

	CoboWaas2 "github.com/CoboGlobal/cobo-waas2-go-sdk/cobo_waas2"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/uuid"
)

type Usecase struct {
	client *portal.Client
	repo   Repo
	ucw    *conf.UCW
	logger *log.Helper
}

func NewUsecase(
	ucw *conf.UCW,
	logger log.Logger,
	client *portal.Client,
	repo Repo) *Usecase {
	return &Usecase{
		client: client,
		repo:   repo,
		ucw:    ucw,
		logger: log.NewHelper(logger),
	}
}

func (u Usecase) CreateTransaction(ctx context.Context, transaction *Transaction) (string, error) {
	transaction.TransactionID = uuid.New().String()
	transaction.Status = int64(v1.Transaction_STATUS_CREATED)
	if _, err := u.repo.Save(ctx, transaction); err != nil {
		return "", err
	}
	id, err := u.SubmitTransaction(ctx, transaction)
	if err != nil {
		transaction.Status = int64(v1.Transaction_STATUS_FAILED)
		return "", u.repo.Update(ctx, *transaction)
	}
	return id, nil
}

func (u Usecase) SubmitTransaction(ctx context.Context, transaction *Transaction) (string, error) {
	switch v1.Transaction_Type(transaction.Type) {
	case v1.Transaction_WITHDRAW:
		res, _, err := u.client.TransactionsAPI.CreateTransferTransaction(u.client.WithContext(ctx)).TransferParams(CoboWaas2.TransferParams{
			RequestId: transaction.TransactionID,
			Source: CoboWaas2.TransferSource{
				MpcTransferSource: &CoboWaas2.MpcTransferSource{
					SourceType: CoboWaas2.WALLETSUBTYPE_USER_CONTROLLED,
					WalletId:   transaction.WalletID,
					Address:    CoboWaas2.PtrString(transaction.From),
				},
				SafeTransferSource: nil,
			},
			TokenId: transaction.TokenID,
			Destination: CoboWaas2.TransferDestination{
				AddressTransferDestination: &CoboWaas2.AddressTransferDestination{
					DestinationType: CoboWaas2.TRANSFERDESTINATIONTYPE_ADDRESS,
					AccountOutput: &CoboWaas2.AddressTransferDestinationAccountOutput{
						Address: transaction.To,
						Memo:    nil,
						Amount:  transaction.Amount.String(),
					},
				},
			},
			CategoryNames: nil,
			Description:   nil,
			Fee: &CoboWaas2.TransactionRequestFee{
				TransactionRequestEvmEip1559Fee: transaction.GetFee().ToEvmEip1559TransactionFee(),
				TransactionRequestEvmLegacyFee:  transaction.GetFee().ToEvmLegacyTransactionFee(),
				TransactionRequestFixedFee:      nil,
				TransactionRequestUtxoFee:       transaction.GetFee().ToUtxoTransactionFee(),
			},
		}).Execute()
		if err != nil {
			u.logger.Errorf("SubmitTransaction CreateTransferTransaction transaction id %s, chain %s, from %s, to %s, type %d, err: %v",
				transaction.TransactionID, transaction.Chain, transaction.From, transaction.To, transaction.Type, err)
			return "", err
		}
		transaction.ExternalID = sql.NullString{
			String: res.GetTransactionId(),
			Valid:  true,
		}
		transaction.Status = int64(v1.Transaction_STATUS_SUBMITTED)
		return transaction.TransactionID, u.repo.Update(ctx, *transaction)
	}
	return transaction.TransactionID, v1.ErrorUnsupportedTransactionType("transaction type %v", transaction.Type)
}

func (u Usecase) PrepareTransaction(ctx context.Context, transaction *Transaction) (*EstimatedFee, error) {
	switch v1.Transaction_Type(transaction.Type) {
	case v1.Transaction_WITHDRAW:
		res, _, err := u.client.TransactionsAPI.EstimateFee(u.client.WithContext(ctx)).EstimateFeeParams(CoboWaas2.EstimateFeeParams{
			EstimateContractCallFeeParams: nil,
			EstimateTransferFeeParams: &CoboWaas2.EstimateTransferFeeParams{
				RequestId:   CoboWaas2.PtrString(uuid.New().String()),
				RequestType: CoboWaas2.ESTIMATEFEEREQUESTTYPE_TRANSFER,
				Source: CoboWaas2.TransferSource{
					CustodialTransferSource: nil,
					ExchangeTransferSource:  nil,
					MpcTransferSource: &CoboWaas2.MpcTransferSource{
						SourceType:    CoboWaas2.WALLETSUBTYPE_USER_CONTROLLED,
						WalletId:      transaction.WalletID,
						Address:       CoboWaas2.PtrString(transaction.From),
						IncludedUtxos: nil,
						ExcludedUtxos: nil,
					},
					SafeTransferSource: nil,
				},
				TokenId: transaction.TokenID,
				Destination: &CoboWaas2.TransferDestination{
					AddressTransferDestination: &CoboWaas2.AddressTransferDestination{
						DestinationType: CoboWaas2.TRANSFERDESTINATIONTYPE_ADDRESS,
						AccountOutput:   nil,
						UtxoOutputs:     nil,
						ChangeAddress:   nil,
						ForceInternal:   nil,
						ForceExternal:   nil,
					},
					ExchangeTransferDestination: nil,
				},
				FeeType: CoboWaas2.FEETYPE_EVM_LEGACY.Ptr(),
			},
		}).Execute()
		if err != nil {
			u.logger.Errorf("PrepareTransaction EstimateFee transaction id %s, chain %s, from %s, to %s, type %d, err: %v",
				transaction.TransactionID, transaction.Chain, transaction.From, transaction.To, transaction.Type, err)
			return nil, err
		}

		estimated, err := BuildEstimatedFeeFromPortal(res)
		if err != nil {
			return nil, err
		}
		return estimated, nil
	}
	return nil, v1.ErrorUnsupportedTransactionType("transaction type %v", transaction.Type)
}

func (u Usecase) ListTransaction(ctx context.Context, params ListTransactionParams) ([]*Transaction, error) {
	return u.repo.ListTransactions(ctx, params)
}

func (u Usecase) GetTransaction(ctx context.Context, transactionID string) (*Transaction, error) {
	return u.repo.GetByTransactionID(ctx, transactionID)
}

func (u Usecase) SyncDepositTransaction(ctx context.Context, transaction *Transaction) (*Transaction, error) {
	transactions, err := u.repo.ListTransactions(ctx, ListTransactionParams{
		WalletID:        transaction.WalletID,
		TokenID:         transaction.TokenID,
		TransactionType: v1.Transaction_DEPOSIT,
		ExternalID:      transaction.ExternalID.String,
	})
	if err != nil {
		return nil, err
	}

	if len(transactions) == 1 {
		return transactions[0], nil
	}
	transaction.TransactionID = uuid.New().String()
	if _, err = u.repo.Save(ctx, transaction); err != nil {
		return nil, err
	}
	return transaction, err
}

func (u Usecase) SyncTransactionTask() error {
	var lastID = int64(-1)
	ctx := context.Background()
	limit := 100
	for lastID != 0 {
		list, err := u.repo.ListTransactions(ctx, ListTransactionParams{
			TransactionType: v1.Transaction_WITHDRAW,
			Limit:           limit,
			LastID:          lastID,
			Status: []v1.Transaction_Status{
				v1.Transaction_STATUS_SUBMITTED,
				v1.Transaction_STATUS_PENDING_SCREENING,
				v1.Transaction_STATUS_CONFIRMING,
				v1.Transaction_STATUS_PENDING_SIGNATURE,
				v1.Transaction_STATUS_BROADCASTING,
				v1.Transaction_STATUS_PENDING,
				v1.Transaction_STATUS_QUEUED,
				v1.Transaction_STATUS_PENDING_AUTHORIZATION,
			},
		})
		if err != nil {
			return err
		}

		if len(list) == limit {
			lastID = int64(list[len(list)-1].ID)
		} else {
			lastID = 0
		}

		if err := u.SyncTransactions(ctx, list); err != nil {
			return err
		}
	}
	return nil
}

func (u Usecase) SubmitCreatedTransactionTask() error {
	var lastID = int64(-1)
	ctx := context.Background()
	limit := 100
	for lastID != 0 {
		list, err := u.repo.ListTransactions(ctx, ListTransactionParams{
			TransactionType: v1.Transaction_WITHDRAW,
			Limit:           limit,
			LastID:          lastID,
			Status: []v1.Transaction_Status{
				v1.Transaction_STATUS_CREATED,
			},
		})
		if err != nil {
			return err
		}

		if len(list) == limit {
			lastID = int64(list[len(list)-1].ID)
		} else {
			lastID = 0
		}

		for _, each := range list {
			if _, err := u.SubmitTransaction(ctx, each); err != nil {
				return err
			}
		}
	}
	return nil
}

func (u Usecase) SyncTransactions(ctx context.Context, transactions []*Transaction) error {
	if len(transactions) == 0 {
		return nil
	}
	coboTransactionIDs := make([]string, 0, len(transactions))

	transactionSet := make(map[string]*Transaction)
	for _, each := range transactions {
		if each.ExternalID.String == "" {
			continue
		}
		coboTransactionIDs = append(coboTransactionIDs, each.ExternalID.String)
		transactionSet[each.ExternalID.String] = each
	}
	response, _, err := u.client.TransactionsAPI.ListTransactions(u.client.WithContext(ctx)).TransactionIds(strings.Join(coboTransactionIDs, ",")).Execute()
	if err != nil {
		u.logger.Errorf("SyncTransactions ListTransactions transaction ids %v, err: %v", coboTransactionIDs, err)
		return err
	}

	for _, each := range response.GetData() {

		u.logger.Infof("requestID %v, coboID %v, status %v, subStatus %v, transactionID %v", each.RequestId, each.GetCoboId(), each.GetStatus(), each.GetSubStatus(), each.GetTransactionId())
		set := transactionSet[each.GetTransactionId()].ApplyStatus(&each)
		if !set {
			continue
		}
		u.logger.Infof("ApplyStatus")
		transactionSet[each.GetTransactionId()].ApplyTxHash(&each)
		u.logger.Infof("ApplyTxHash")
		transactionSet[each.GetTransactionId()].ApplyExtra(&each)
		u.logger.Infof("ApplyExtra")
		if err := transactionSet[each.GetTransactionId()].ApplyFeeUsed(&each); err != nil {
			return err
		}
		if err := u.repo.Update(ctx, *transactionSet[each.GetTransactionId()]); err != nil {
			return err
		}
	}

	return nil
}

func (u Usecase) RejectTransaction(ctx context.Context, transactionID string) error {
	tx, err := u.repo.GetByTransactionID(ctx, transactionID)
	if err != nil {
		return err
	}

	tx.Status = int64(v1.Transaction_STATUS_REJECTED)
	return u.repo.Update(ctx, *tx)
}

func (u Usecase) ApproveTransaction(ctx context.Context, transactionID string) error {
	tx, err := u.repo.GetByTransactionID(ctx, transactionID)
	if err != nil {
		return err
	}
	tx.SubStatus = int64(v1.Transaction_SUB_STATUS_PENDING_SIGNATURE_HAS_APPROVED)
	return u.repo.Update(ctx, *tx)
}
