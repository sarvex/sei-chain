package abci

import (
	"context"
	"encoding/json"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/keeper/utils"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	typesutils "github.com/sei-protocol/sei-chain/x/dex/types/utils"
	"github.com/sei-protocol/sei-chain/x/dex/types/wasm"
	"go.opentelemetry.io/otel/attribute"
	otrace "go.opentelemetry.io/otel/trace"
)

// There is a limit on how many bytes can be passed to wasm VM in a single call,
// so we shouldn't bump this number unless there is an upgrade to wasm VM
const MaxOrdersPerSudoCall = 50000

func (w KeeperWrapper) HandleEBPlaceOrders(ctx context.Context, sdkCtx sdk.Context, tracer *otrace.Tracer, contractAddr string, registeredPairs []types.Pair) error {
	_, span := (*tracer).Start(ctx, "SudoPlaceOrders")
	span.SetAttributes(attribute.String("contractAddr", contractAddr))
	defer span.End()

	typedContractAddr := typesutils.ContractAddress(contractAddr)
	msgs := w.GetPlaceSudoMsg(sdkCtx, typedContractAddr, registeredPairs)

	responses := []wasm.SudoOrderPlacementResponse{}

	for _, msg := range msgs {
		data, err := utils.CallContractSudo(sdkCtx, w.Keeper, contractAddr, msg)
		if err != nil {
			sdkCtx.Logger().Error(fmt.Sprintf("Error during order placement: %s", err.Error()))
			return err
		}
		response := wasm.SudoOrderPlacementResponse{}
		if err := json.Unmarshal(data, &response); err != nil {
			sdkCtx.Logger().Error("Failed to parse order placement response")
			return err
		}
		sdkCtx.Logger().Info(fmt.Sprintf("Sudo response data: %s", response))
		responses = append(responses, response)
	}

	for _, pair := range registeredPairs {
		typedPairStr := typesutils.GetPairString(&pair) //nolint:gosec // USING THE POINTER HERE COULD BE BAD, LET'S CHECK IT.
		for _, response := range responses {
			w.MemState.GetBlockOrders(sdkCtx, typedContractAddr, typedPairStr).MarkFailedToPlace(response.UnsuccessfulOrders)
		}
	}
	return nil
}

func (w KeeperWrapper) GetPlaceSudoMsg(ctx sdk.Context, typedContractAddr typesutils.ContractAddress, registeredPairs []types.Pair) []wasm.SudoOrderPlacementMsg {
	msgs := []wasm.SudoOrderPlacementMsg{}
	contractOrderPlacements := []types.Order{}
	for _, pair := range registeredPairs {
		typedPairStr := typesutils.GetPairString(&pair) //nolint:gosec // USING THE POINTER HERE COULD BE BAD, LET'S CHECK IT.
		for _, order := range w.MemState.GetBlockOrders(ctx, typedContractAddr, typedPairStr).Get() {
			contractOrderPlacements = append(contractOrderPlacements, *order)
			if len(contractOrderPlacements) == MaxOrdersPerSudoCall {
				msgs = append(msgs, wasm.SudoOrderPlacementMsg{
					OrderPlacements: wasm.OrderPlacementMsgDetails{
						Orders:   contractOrderPlacements,
						Deposits: []wasm.ContractDepositInfo{},
					},
				})
				contractOrderPlacements = []types.Order{}
			}
		}
	}
	msgs = append(msgs, wasm.SudoOrderPlacementMsg{
		OrderPlacements: wasm.OrderPlacementMsgDetails{
			Orders:   contractOrderPlacements,
			Deposits: []wasm.ContractDepositInfo{},
		},
	})
	return msgs
}
