package blockchain

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/multiversx/mx-chain-core-go/core"
	"github.com/multiversx/mx-chain-core-go/core/check"
	"github.com/multiversx/mx-chain-core-go/data/api"
	"github.com/multiversx/mx-chain-core-go/data/transaction"
	"github.com/multiversx/mx-chain-go/config"
	"github.com/multiversx/mx-chain-go/state"
	sdkCore "github.com/multiversx/mx-sdk-go/core"
	sdkHttp "github.com/multiversx/mx-sdk-go/core/http"
	"github.com/multiversx/mx-sdk-go/data"
	"github.com/multiversx/mx-sdk-go/testsCommon"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testHttpURL = "https://test.org"
const networkConfigEndpoint = "network/config"
const getNetworkStatusEndpoint = "network/status/%d"
const getNodeStatusEndpoint = "node/status"

// not a real-world valid test query option but rather a test one to check all fields are properly set
var testQueryOptions = api.AccountQueryOptions{
	OnFinalBlock: true,
	OnStartOfEpoch: core.OptionalUint32{
		Value:    3737,
		HasValue: true,
	},
	BlockNonce: core.OptionalUint64{
		Value:    3838,
		HasValue: true,
	},
	BlockHash:     []byte("block hash"),
	BlockRootHash: []byte("block root hash"),
	HintEpoch: core.OptionalUint32{
		Value:    3939,
		HasValue: true,
	},
}

type testStruct struct {
	Nonce int
	Name  string
}

type mockHTTPClient struct {
	doCalled func(req *http.Request) (*http.Response, error)
}

func (m *mockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	if m.doCalled != nil {
		return m.doCalled(req)
	}

	return nil, errors.New("not implemented")
}

func createMockClientRespondingBytes(responseBytes []byte) *mockHTTPClient {
	return &mockHTTPClient{
		doCalled: func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				Body:       ioutil.NopCloser(bytes.NewReader(responseBytes)),
				StatusCode: http.StatusOK,
			}, nil
		},
	}
}

func createMockClientRespondingBytesWithStatus(responseBytes []byte, status int) *mockHTTPClient {
	return &mockHTTPClient{
		doCalled: func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				Body:       ioutil.NopCloser(bytes.NewReader(responseBytes)),
				StatusCode: status,
			}, nil
		},
	}
}

func createMockClientRespondingError(err error) *mockHTTPClient {
	return &mockHTTPClient{
		doCalled: func(req *http.Request) (*http.Response, error) {
			return nil, err
		},
	}
}

func createMockArgsProxy(httpClient sdkHttp.Client) ArgsProxy {
	return ArgsProxy{
		ProxyURL:            testHttpURL,
		Client:              httpClient,
		SameScState:         false,
		ShouldBeSynced:      false,
		FinalityCheck:       false,
		AllowedDeltaToFinal: 1,
		CacheExpirationTime: time.Second,
		EntityType:          sdkCore.ObserverNode,
	}
}

func handleRequestNetworkConfigAndStatus(
	req *http.Request,
	numShards uint32,
	currentNonce uint64,
	highestNonce uint64,
) (*http.Response, bool, error) {

	handled := false
	url := req.URL.String()
	var response interface{}
	switch url {
	case fmt.Sprintf("%s/%s", testHttpURL, networkConfigEndpoint):
		handled = true
		response = data.NetworkConfigResponse{
			Data: struct {
				Config *data.NetworkConfig `json:"config"`
			}{
				Config: &data.NetworkConfig{
					NumShardsWithoutMeta: numShards,
				},
			},
		}

	case fmt.Sprintf("%s/%s", testHttpURL, getNodeStatusEndpoint):
		handled = true
		response = data.NodeStatusResponse{
			Data: struct {
				Status *data.NetworkStatus `json:"metrics"`
			}{
				Status: &data.NetworkStatus{
					Nonce:                currentNonce,
					HighestNonce:         highestNonce,
					ProbableHighestNonce: currentNonce,
					ShardID:              2,
				},
			},
			Error: "",
			Code:  "",
		}
	case fmt.Sprintf("%s/%s", testHttpURL, fmt.Sprintf(getNetworkStatusEndpoint, 2)):
		handled = true
		response = data.NetworkStatusResponse{
			Data: struct {
				Status *data.NetworkStatus `json:"status"`
			}{
				Status: &data.NetworkStatus{
					Nonce:                currentNonce,
					HighestNonce:         highestNonce,
					ProbableHighestNonce: currentNonce,
					ShardID:              2,
				},
			},
			Error: "",
			Code:  "",
		}
	}

	if !handled {
		return nil, handled, nil
	}

	buff, _ := json.Marshal(response)
	return &http.Response{
		Body:       ioutil.NopCloser(bytes.NewReader(buff)),
		StatusCode: http.StatusOK,
	}, handled, nil
}

func TestNewProxy(t *testing.T) {
	t.Parallel()

	t.Run("invalid time cache should error", func(t *testing.T) {
		t.Parallel()

		args := createMockArgsProxy(nil)
		args.CacheExpirationTime = time.Second - time.Nanosecond
		proxyInstance, err := NewProxy(args)

		assert.True(t, check.IfNil(proxyInstance))
		assert.True(t, errors.Is(err, ErrInvalidCacherDuration))
	})
	t.Run("invalid nonce delta should error", func(t *testing.T) {
		t.Parallel()

		args := createMockArgsProxy(nil)
		args.FinalityCheck = true
		args.AllowedDeltaToFinal = 0
		proxyInstance, err := NewProxy(args)

		assert.True(t, check.IfNil(proxyInstance))
		assert.True(t, errors.Is(err, ErrInvalidAllowedDeltaToFinal))
	})
	t.Run("should work with finality check", func(t *testing.T) {
		t.Parallel()

		args := createMockArgsProxy(nil)
		args.FinalityCheck = true
		proxyInstance, err := NewProxy(args)

		assert.False(t, check.IfNil(proxyInstance))
		assert.Nil(t, err)
	})
	t.Run("should work without finality check", func(t *testing.T) {
		t.Parallel()

		args := createMockArgsProxy(nil)
		proxyInstance, err := NewProxy(args)

		assert.False(t, check.IfNil(proxyInstance))
		assert.Nil(t, err)
	})
}

func TestGetAccount(t *testing.T) {
	t.Parallel()

	numAccountQueries := uint32(0)
	httpClient := &mockHTTPClient{
		doCalled: func(req *http.Request) (*http.Response, error) {
			response, handled, err := handleRequestNetworkConfigAndStatus(req, 3, 9170526, 9170526)
			if handled {
				return response, err
			}

			account := data.AccountResponse{
				Data: struct {
					Account *data.Account `json:"account"`
				}{
					Account: &data.Account{
						Nonce:   37,
						Balance: "38",
					},
				},
			}
			accountBytes, _ := json.Marshal(account)
			atomic.AddUint32(&numAccountQueries, 1)
			return &http.Response{
				Body:       ioutil.NopCloser(bytes.NewReader(accountBytes)),
				StatusCode: http.StatusOK,
			}, nil
		},
	}
	args := createMockArgsProxy(httpClient)
	args.FinalityCheck = true
	proxyInstance, _ := NewProxy(args)

	address, err := data.NewAddressFromBech32String("erd1qqqqqqqqqqqqqpgqfzydqmdw7m2vazsp6u5p95yxz76t2p9rd8ss0zp9ts")
	if err != nil {
		assert.Error(t, err)
	}
	expectedErr := errors.New("expected error")

	t.Run("finality checker errors should not query", func(t *testing.T) {
		proxyInstance.finalityProvider = &testsCommon.FinalityProviderStub{
			CheckShardFinalizationCalled: func(ctx context.Context, targetShardID uint32, maxNoncesDelta uint64) error {
				return expectedErr
			},
		}

		account, errGet := proxyInstance.GetAccount(context.Background(), address)
		assert.Nil(t, account)
		assert.True(t, errors.Is(errGet, expectedErr))
		assert.Equal(t, uint32(0), atomic.LoadUint32(&numAccountQueries))
	})
	t.Run("finality checker returns nil should return account", func(t *testing.T) {
		finalityCheckCalled := uint32(0)
		proxyInstance.finalityProvider = &testsCommon.FinalityProviderStub{
			CheckShardFinalizationCalled: func(ctx context.Context, targetShardID uint32, maxNoncesDelta uint64) error {
				atomic.AddUint32(&finalityCheckCalled, 1)
				return nil
			},
		}

		account, errGet := proxyInstance.GetAccount(context.Background(), address)
		assert.NotNil(t, account)
		assert.Equal(t, uint64(37), account.Nonce)
		assert.Nil(t, errGet)
		assert.Equal(t, uint32(1), atomic.LoadUint32(&numAccountQueries))
		assert.Equal(t, uint32(1), atomic.LoadUint32(&finalityCheckCalled))
	})
}

func TestProxy_GetNetworkEconomics(t *testing.T) {
	t.Parallel()

	responseBytes := []byte(`{"data":{"metrics":{"erd_dev_rewards":"0","erd_epoch_for_economics_data":263,"erd_inflation":"5869888769785838708144","erd_total_fees":"51189055176110000000","erd_total_staked_value":"9963775651405816710680128","erd_total_supply":"21556417261819025351089574","erd_total_top_up_value":"1146275808171377418645274"}},"code":"successful"}`)
	httpClient := createMockClientRespondingBytes(responseBytes)
	args := createMockArgsProxy(httpClient)
	ep, _ := NewProxy(args)

	networkEconomics, err := ep.GetNetworkEconomics(context.Background())
	require.Nil(t, err)
	require.Equal(t, &data.NetworkEconomics{
		DevRewards:            "0",
		EpochForEconomicsData: 263,
		Inflation:             "5869888769785838708144",
		TotalFees:             "51189055176110000000",
		TotalStakedValue:      "9963775651405816710680128",
		TotalSupply:           "21556417261819025351089574",
		TotalTopUpValue:       "1146275808171377418645274",
	}, networkEconomics)
}

func TestProxy_RequestTransactionCost(t *testing.T) {
	t.Parallel()

	responseBytes := []byte(`{"data":{"txGasUnits":24273810,"returnMessage":""},"error":"","code":"successful"}`)
	httpClient := createMockClientRespondingBytes(responseBytes)
	args := createMockArgsProxy(httpClient)
	ep, _ := NewProxy(args)

	tx := &transaction.FrontendTransaction{
		Nonce:    1,
		Value:    "50",
		Receiver: "erd1rh5ws22jxm9pe7dtvhfy6j3uttuupkepferdwtmslms5fydtrh5sx3xr8r",
		Sender:   "erd1rh5ws22jxm9pe7dtvhfy6j3uttuupkepferdwtmslms5fydtrh5sx3xr8r",
		Data:     []byte("hello"),
		ChainID:  "1",
		Version:  1,
		Options:  0,
	}
	txCost, err := ep.RequestTransactionCost(context.Background(), tx)
	require.Nil(t, err)
	require.Equal(t, &data.TxCostResponseData{
		TxCost:     24273810,
		RetMessage: "",
	}, txCost)
}

func TestProxy_GetTransactionInfoWithResults(t *testing.T) {
	t.Parallel()

	responseBytes := []byte(`{"data":{"transaction":{"type":"normal","nonce":22100,"round":53057,"epoch":263,"value":"0","receiver":"erd1e6c9vcga5lyhwdu9nr9lya4ujz2r5w2egsfjp0lslrgv5dsccpdsmre6va","sender":"erd1e6c9vcga5lyhwdu9nr9lya4ujz2r5w2egsfjp0lslrgv5dsccpdsmre6va","gasPrice":1000000001,"gasLimit":19210000,"data":"RVNEVE5GVFRyYW5zZmVyQDU5NTk1OTQ1MzY0MzM5MkQzMDM5MzQzOTMwMzBAMDFAMDFAMDAwMDAwMDAwMDAwMDAwMDA1MDA1QzgzRTBDNDJFRENFMzk0RjQwQjI0RDI5RDI5OEIwMjQ5QzQxRjAyODk3NEA2Njc1NkU2NEA1N0M1MDZBMTlEOEVBRTE4Mjk0MDNBOEYzRjU0RTFGMDM3OUYzODE1N0ZDRjUzRDVCQ0E2RjIzN0U0QTRDRjYxQDFjMjA=","signature":"37922ccb13d46857d819cd618f1fd8e76777e14f1c16a54eeedd55c67d5883db0e3a91cd0ff0f541e2c3d7347488b4020c0ba4765c9bc02ea0ae1c3f6db1ec05","sourceShard":1,"destinationShard":1,"blockNonce":53052,"blockHash":"4a63312d1bfe48aa516185d12abff5daf6343fce1f298db51291168cf97a790c","notarizedAtSourceInMetaNonce":53053,"NotarizedAtSourceInMetaHash":"342d189e36ef5cbf9f8b3f2cb5bf2cb8e2260062c6acdf89ce5faacd99f4dbcc","notarizedAtDestinationInMetaNonce":53053,"notarizedAtDestinationInMetaHash":"342d189e36ef5cbf9f8b3f2cb5bf2cb8e2260062c6acdf89ce5faacd99f4dbcc","miniblockType":"TxBlock","miniblockHash":"0c659cce5e2653522cc0e3cf35571264522035a7aef4ffa5244d1ed3d8bc01a8","status":"success","hyperblockNonce":53053,"hyperblockHash":"342d189e36ef5cbf9f8b3f2cb5bf2cb8e2260062c6acdf89ce5faacd99f4dbcc","smartContractResults":[{"hash":"5ab14959aaeb3a20d95ec6bbefc03f251732d9368711c55c63d3811e70903f4e","nonce":0,"value":0,"receiver":"erd1qqqqqqqqqqqqqpgqtjp7p3pwmn3efaqtynff62vtqfyug8cz396qs5vnsy","sender":"erd1e6c9vcga5lyhwdu9nr9lya4ujz2r5w2egsfjp0lslrgv5dsccpdsmre6va","data":"ESDTNFTTransfer@595959453643392d303934393030@01@01@080112020001226f08011204746573741a20ceb056611da7c977378598cbf276bc90943a3959441320bff0f8d0ca3618c05b20e8072a206161616161616161616161616161616161616161616161616161616161616161320461626261321268747470733a2f2f656c726f6e642e636f6d3a0474657374@66756e64@57c506a19d8eae1829403a8f3f54e1f0379f38157fcf53d5bca6f237e4a4cf61@1c20","prevTxHash":"c7fadeaccce0673bd6ce3a1f472f7cc1beef20c0b3131cfa9866cd5075816639","originalTxHash":"c7fadeaccce0673bd6ce3a1f472f7cc1beef20c0b3131cfa9866cd5075816639","gasLimit":18250000,"gasPrice":1000000001,"callType":0},{"hash":"f337d2705d2b644f9d8f75cf270e879b6ada51c4c54009e94305adf368d1adbc","nonce":1,"value":102793170000000,"receiver":"erd1e6c9vcga5lyhwdu9nr9lya4ujz2r5w2egsfjp0lslrgv5dsccpdsmre6va","sender":"erd1qqqqqqqqqqqqqpgqtjp7p3pwmn3efaqtynff62vtqfyug8cz396qs5vnsy","data":"@6f6b","prevTxHash":"5ab14959aaeb3a20d95ec6bbefc03f251732d9368711c55c63d3811e70903f4e","originalTxHash":"c7fadeaccce0673bd6ce3a1f472f7cc1beef20c0b3131cfa9866cd5075816639","gasLimit":0,"gasPrice":1000000001,"callType":0}]}},"error":"","code":"successful"}`)
	httpClient := createMockClientRespondingBytes(responseBytes)
	args := createMockArgsProxy(httpClient)
	ep, _ := NewProxy(args)

	tx, err := ep.GetTransactionInfoWithResults(context.Background(), "a40e5a6af4efe221608297a73459211756ab88b96896e6e331842807a138f343")
	require.Nil(t, err)

	txBytes, _ := json.MarshalIndent(tx, "", " ")
	fmt.Println(string(txBytes))
}

func TestProxy_ExecuteVmQuery(t *testing.T) {
	t.Parallel()

	responseBytes := []byte(`{"data":{"data":{"returnData":["MC41LjU="],"returnCode":"ok","returnMessage":"","gasRemaining":18446744073685949187,"gasRefund":0,"outputAccounts":{"0000000000000000050033bb65a91ee17ab84c6f8a01846ef8644e15fb76696a":{"address":"erd1qqqqqqqqqqqqqpgqxwakt2g7u9atsnr03gqcgmhcv38pt7mkd94q6shuwt","nonce":0,"balance":null,"balanceDelta":0,"storageUpdates":{},"code":null,"codeMetaData":null,"outputTransfers":[],"callType":0}},"deletedAccounts":[],"touchedAccounts":[],"logs":[]}},"error":"","code":"successful"}`)
	t.Run("no finality check", func(t *testing.T) {
		httpClient := createMockClientRespondingBytes(responseBytes)
		args := createMockArgsProxy(httpClient)
		ep, _ := NewProxy(args)

		response, err := ep.ExecuteVMQuery(context.Background(), &data.VmValueRequest{
			Address:    "erd1qqqqqqqqqqqqqpgqxwakt2g7u9atsnr03gqcgmhcv38pt7mkd94q6shuwt",
			FuncName:   "version",
			CallerAddr: "erd1qqqqqqqqqqqqqpgqxwakt2g7u9atsnr03gqcgmhcv38pt7mkd94q6shuwt",
		})
		require.Nil(t, err)
		require.Equal(t, "0.5.5", string(response.Data.ReturnData[0]))
	})
	t.Run("with finality check, chain is stuck", func(t *testing.T) {
		httpClient := &mockHTTPClient{
			doCalled: func(req *http.Request) (*http.Response, error) {
				response, handled, err := handleRequestNetworkConfigAndStatus(req, 3, 9170528, 9170526)
				if handled {
					return response, err
				}

				assert.Fail(t, "should have not reached this point in which the VM query is actually requested")
				return nil, nil
			},
		}
		args := createMockArgsProxy(httpClient)
		args.FinalityCheck = true
		ep, _ := NewProxy(args)

		response, err := ep.ExecuteVMQuery(context.Background(), &data.VmValueRequest{
			Address:    "erd1qqqqqqqqqqqqqpgqxwakt2g7u9atsnr03gqcgmhcv38pt7mkd94q6shuwt",
			FuncName:   "version",
			CallerAddr: "erd1qqqqqqqqqqqqqpgqxwakt2g7u9atsnr03gqcgmhcv38pt7mkd94q6shuwt",
		})

		assert.NotNil(t, err)
		assert.True(t, strings.Contains(err.Error(), "shardID 2 is stuck"))
		assert.Nil(t, response)
	})
	t.Run("with finality check, invalid address", func(t *testing.T) {
		httpClient := &mockHTTPClient{
			doCalled: func(req *http.Request) (*http.Response, error) {
				response, handled, err := handleRequestNetworkConfigAndStatus(req, 3, 9170526, 9170526)
				if handled {
					return response, err
				}

				assert.Fail(t, "should have not reached this point in which the VM query is actually requested")
				return nil, nil
			},
		}
		args := createMockArgsProxy(httpClient)
		args.FinalityCheck = true
		ep, _ := NewProxy(args)

		response, err := ep.ExecuteVMQuery(context.Background(), &data.VmValueRequest{
			Address:    "invalid",
			FuncName:   "version",
			CallerAddr: "erd1qqqqqqqqqqqqqpgqxwakt2g7u9atsnr03gqcgmhcv38pt7mkd94q6shuwt",
		})

		assert.NotNil(t, err)
		assert.True(t, strings.Contains(err.Error(), "invalid bech32 string length 7"))
		assert.Nil(t, response)
	})
	t.Run("with finality check, should work", func(t *testing.T) {
		wasHandled := false
		httpClient := &mockHTTPClient{
			doCalled: func(req *http.Request) (*http.Response, error) {
				response, handled, err := handleRequestNetworkConfigAndStatus(req, 3, 9170526, 9170525)
				if handled {
					wasHandled = true
					return response, err
				}

				return &http.Response{
					Body:       ioutil.NopCloser(bytes.NewReader(responseBytes)),
					StatusCode: http.StatusOK,
				}, nil
			},
		}
		args := createMockArgsProxy(httpClient)
		args.FinalityCheck = true
		ep, _ := NewProxy(args)

		response, err := ep.ExecuteVMQuery(context.Background(), &data.VmValueRequest{
			Address:    "erd1qqqqqqqqqqqqqpgqxwakt2g7u9atsnr03gqcgmhcv38pt7mkd94q6shuwt",
			FuncName:   "version",
			CallerAddr: "erd1qqqqqqqqqqqqqpgqxwakt2g7u9atsnr03gqcgmhcv38pt7mkd94q6shuwt",
		})

		assert.True(t, wasHandled)
		require.Nil(t, err)
		require.Equal(t, "0.5.5", string(response.Data.ReturnData[0]))
	})
}

func TestProxy_GetRawBlockByHash(t *testing.T) {
	t.Parallel()

	expectedTs := &testStruct{
		Nonce: 1,
		Name:  "a test struct to be sent and received",
	}
	responseBytes, _ := json.Marshal(expectedTs)
	rawBlockData := &data.RawBlockRespone{
		Data: struct {
			Block []byte "json:\"block\""
		}{
			Block: responseBytes,
		},
		Error: "",
		Code:  "200",
	}
	rawBlockDataBytes, _ := json.Marshal(rawBlockData)

	httpClient := createMockClientRespondingBytes(rawBlockDataBytes)
	args := createMockArgsProxy(httpClient)
	ep, _ := NewProxy(args)

	response, err := ep.GetRawBlockByHash(context.Background(), 0, "aaaa")
	require.Nil(t, err)

	ts := &testStruct{}
	err = json.Unmarshal(response, ts)
	require.Nil(t, err)
	require.Equal(t, expectedTs, ts)
}

func TestProxy_GetRawBlockByNonce(t *testing.T) {
	t.Parallel()

	expectedTs := &testStruct{
		Nonce: 10,
		Name:  "a test struct to be sent and received",
	}
	responseBytes, _ := json.Marshal(expectedTs)
	rawBlockData := &data.RawBlockRespone{
		Data: struct {
			Block []byte "json:\"block\""
		}{
			Block: responseBytes,
		},
	}
	rawBlockDataBytes, _ := json.Marshal(rawBlockData)

	httpClient := createMockClientRespondingBytes(rawBlockDataBytes)
	args := createMockArgsProxy(httpClient)
	ep, _ := NewProxy(args)

	response, err := ep.GetRawBlockByNonce(context.Background(), 0, 10)
	require.Nil(t, err)

	ts := &testStruct{}
	err = json.Unmarshal(response, ts)
	require.Nil(t, err)
	require.Equal(t, expectedTs, ts)
}

func TestProxy_GetRawMiniBlockByHash(t *testing.T) {
	t.Parallel()

	expectedTs := &testStruct{
		Nonce: 10,
		Name:  "a test struct to be sent and received",
	}
	responseBytes, _ := json.Marshal(expectedTs)
	rawBlockData := &data.RawMiniBlockRespone{
		Data: struct {
			MiniBlock []byte "json:\"miniblock\""
		}{
			MiniBlock: responseBytes,
		},
	}
	rawBlockDataBytes, _ := json.Marshal(rawBlockData)

	httpClient := createMockClientRespondingBytes(rawBlockDataBytes)
	args := createMockArgsProxy(httpClient)
	ep, _ := NewProxy(args)

	response, err := ep.GetRawMiniBlockByHash(context.Background(), 0, "aaaa", 1)
	require.Nil(t, err)

	ts := &testStruct{}
	err = json.Unmarshal(response, ts)
	require.Nil(t, err)
	require.Equal(t, expectedTs, ts)
}

func TestProxy_GetNonceAtEpochStart(t *testing.T) {
	t.Parallel()

	expectedNonce := uint64(2)
	expectedNetworkStatus := &data.NetworkStatus{
		NonceAtEpochStart: expectedNonce,
		ShardID:           core.MetachainShardId,
	}
	statusResponse := &data.NodeStatusResponse{
		Data: struct {
			Status *data.NetworkStatus "json:\"metrics\""
		}{
			Status: expectedNetworkStatus,
		},
	}
	statusResponseBytes, _ := json.Marshal(statusResponse)

	httpClient := createMockClientRespondingBytes(statusResponseBytes)
	args := createMockArgsProxy(httpClient)
	ep, _ := NewProxy(args)

	response, err := ep.GetNonceAtEpochStart(context.Background(), core.MetachainShardId)
	require.Nil(t, err)

	require.Equal(t, expectedNonce, response)
}

func TestProxy_GetRatingsConfig(t *testing.T) {
	t.Parallel()

	expectedRatingsConfig := &data.RatingsConfig{
		GeneralMaxRating: 0,
		GeneralMinRating: 0,
	}
	ratingsResponse := &data.RatingsConfigResponse{
		Data: struct {
			Config *data.RatingsConfig "json:\"config\""
		}{
			Config: expectedRatingsConfig,
		},
	}
	ratingsResponseBytes, _ := json.Marshal(ratingsResponse)

	httpClient := createMockClientRespondingBytes(ratingsResponseBytes)
	args := createMockArgsProxy(httpClient)
	ep, _ := NewProxy(args)

	response, err := ep.GetRatingsConfig(context.Background())
	require.Nil(t, err)

	require.Equal(t, expectedRatingsConfig, response)
}

func TestProxy_GetEnableEpochsConfig(t *testing.T) {
	t.Parallel()

	enableEpochs := config.EnableEpochs{
		BalanceWaitingListsEnableEpoch: 1,
		WaitingListFixEnableEpoch:      1,
	}

	expectedEnableEpochsConfig := &data.EnableEpochsConfig{
		EnableEpochs: enableEpochs,
	}
	enableEpochsResponse := &data.EnableEpochsConfigResponse{
		Data: struct {
			Config *data.EnableEpochsConfig "json:\"enableEpochs\""
		}{
			Config: expectedEnableEpochsConfig},
	}
	enableEpochsResponseBytes, _ := json.Marshal(enableEpochsResponse)

	httpClient := createMockClientRespondingBytes(enableEpochsResponseBytes)
	args := createMockArgsProxy(httpClient)
	ep, _ := NewProxy(args)

	response, err := ep.GetEnableEpochsConfig(context.Background())
	require.Nil(t, err)

	require.Equal(t, expectedEnableEpochsConfig, response)
}

func TestProxy_GetGenesisNodesPubKeys(t *testing.T) {
	t.Parallel()

	expectedGenesisNodes := &data.GenesisNodes{
		Eligible: map[uint32][]string{
			0: {"pubkey1"},
		},
	}
	genesisNodesResponse := &data.GenesisNodesResponse{
		Data: struct {
			Nodes *data.GenesisNodes "json:\"nodes\""
		}{
			Nodes: expectedGenesisNodes},
	}
	genesisNodesResponseBytes, _ := json.Marshal(genesisNodesResponse)

	httpClient := createMockClientRespondingBytes(genesisNodesResponseBytes)
	args := createMockArgsProxy(httpClient)
	ep, _ := NewProxy(args)

	response, err := ep.GetGenesisNodesPubKeys(context.Background())
	require.Nil(t, err)

	require.Equal(t, expectedGenesisNodes, response)
}

func TestProxy_GetValidatorsInfoByEpoch(t *testing.T) {
	t.Parallel()

	expectedValidatorsInfo := []*state.ShardValidatorInfo{
		{
			PublicKey: []byte("pubkey1"),
			ShardId:   1,
		},
	}
	validatorsInfoResponse := &data.ValidatorsInfoResponse{
		Data: struct {
			ValidatorsInfo []*state.ShardValidatorInfo "json:\"validators\""
		}{
			ValidatorsInfo: expectedValidatorsInfo,
		},
	}
	validatorsInfoResponseBytes, _ := json.Marshal(validatorsInfoResponse)

	httpClient := createMockClientRespondingBytes(validatorsInfoResponseBytes)
	args := createMockArgsProxy(httpClient)
	ep, _ := NewProxy(args)

	response, err := ep.GetValidatorsInfoByEpoch(context.Background(), 1)
	require.Nil(t, err)

	require.Equal(t, expectedValidatorsInfo, response)
}

func TestElrondProxy_GetESDTTokenData(t *testing.T) {
	t.Parallel()

	token := "TKN-001122"
	expectedErr := errors.New("expected error")
	validAddress := data.NewAddressFromBytes(bytes.Repeat([]byte("1"), 32))
	emptyQueryOptions := api.AccountQueryOptions{}
	t.Run("nil address, should error", func(t *testing.T) {
		t.Parallel()

		httpClient := createMockClientRespondingBytes(make([]byte, 0))
		args := createMockArgsProxy(httpClient)
		ep, _ := NewProxy(args)

		tokenData, err := ep.GetESDTTokenData(context.Background(), nil, token, emptyQueryOptions)
		assert.Nil(t, tokenData)
		assert.Equal(t, ErrNilAddress, err)
	})
	t.Run("invalid address, should error", func(t *testing.T) {
		t.Parallel()

		httpClient := createMockClientRespondingBytes(make([]byte, 0))
		args := createMockArgsProxy(httpClient)
		ep, _ := NewProxy(args)

		address := data.NewAddressFromBytes([]byte("invalid"))
		tokenData, err := ep.GetESDTTokenData(context.Background(), address, token, emptyQueryOptions)
		assert.Nil(t, tokenData)
		assert.Equal(t, ErrInvalidAddress, err)
	})
	t.Run("http client errors, should error", func(t *testing.T) {
		t.Parallel()

		httpClient := createMockClientRespondingError(expectedErr)
		args := createMockArgsProxy(httpClient)
		ep, _ := NewProxy(args)

		tokenData, err := ep.GetESDTTokenData(context.Background(), validAddress, token, emptyQueryOptions)
		assert.Nil(t, tokenData)
		assert.ErrorIs(t, err, expectedErr)
	})
	t.Run("invalid status, should error", func(t *testing.T) {
		t.Parallel()

		httpClient := createMockClientRespondingBytesWithStatus(make([]byte, 0), http.StatusNotFound)
		args := createMockArgsProxy(httpClient)
		ep, _ := NewProxy(args)

		tokenData, err := ep.GetESDTTokenData(context.Background(), validAddress, token, emptyQueryOptions)
		assert.Nil(t, tokenData)
		assert.ErrorIs(t, err, ErrHTTPStatusCodeIsNotOK)
	})
	t.Run("invalid response bytes, should error", func(t *testing.T) {
		t.Parallel()

		httpClient := createMockClientRespondingBytes([]byte("invalid json"))
		args := createMockArgsProxy(httpClient)
		ep, _ := NewProxy(args)

		tokenData, err := ep.GetESDTTokenData(context.Background(), validAddress, token, emptyQueryOptions)
		assert.Nil(t, tokenData)
		assert.NotNil(t, err)
	})
	t.Run("response returned error, should error", func(t *testing.T) {
		t.Parallel()

		response := &data.ESDTFungibleResponse{
			Error: expectedErr.Error(),
		}
		responseBytes, _ := json.Marshal(response)

		httpClient := createMockClientRespondingBytes(responseBytes)
		args := createMockArgsProxy(httpClient)
		ep, _ := NewProxy(args)

		tokenData, err := ep.GetESDTTokenData(context.Background(), validAddress, token, emptyQueryOptions)
		assert.Nil(t, tokenData)
		assert.NotNil(t, err)
		assert.Equal(t, expectedErr.Error(), err.Error())
	})
	t.Run("should work", func(t *testing.T) {
		t.Parallel()

		responseTokenData := &data.ESDTFungibleTokenData{
			TokenIdentifier: "identifier",
			Balance:         "balance",
			Properties:      "properties",
		}
		response := &data.ESDTFungibleResponse{
			Data: struct {
				TokenData *data.ESDTFungibleTokenData `json:"tokenData"`
			}{
				TokenData: responseTokenData,
			},
		}
		responseBytes, _ := json.Marshal(response)

		httpClient := createMockClientRespondingBytes(responseBytes)
		args := createMockArgsProxy(httpClient)
		ep, _ := NewProxy(args)

		tokenData, err := ep.GetESDTTokenData(context.Background(), validAddress, token, emptyQueryOptions)
		assert.NotNil(t, tokenData)
		assert.Nil(t, err)
		assert.Equal(t, responseTokenData, tokenData)
		assert.False(t, responseTokenData == tokenData) // pointer testing
	})
	t.Run("should work with query options", func(t *testing.T) {
		t.Parallel()

		responseTokenData := &data.ESDTFungibleTokenData{
			TokenIdentifier: "identifier",
			Balance:         "balance",
			Properties:      "properties",
		}
		response := &data.ESDTFungibleResponse{
			Data: struct {
				TokenData *data.ESDTFungibleTokenData `json:"tokenData"`
			}{
				TokenData: responseTokenData,
			},
		}
		responseBytes, _ := json.Marshal(response)
		expectedSuffix := "?blockHash=626c6f636b2068617368&blockNonce=3838&blockRootHash=626c6f636b20726f6f742068617368&hintEpoch=3939&onFinalBlock=true&onStartOfEpoch=3737"

		httpClient := &mockHTTPClient{
			doCalled: func(req *http.Request) (*http.Response, error) {
				assert.True(t, strings.HasSuffix(req.URL.String(), expectedSuffix))

				return &http.Response{
					Body:       ioutil.NopCloser(bytes.NewReader(responseBytes)),
					StatusCode: http.StatusOK,
				}, nil
			},
		}
		args := createMockArgsProxy(httpClient)
		ep, _ := NewProxy(args)

		tokenData, err := ep.GetESDTTokenData(context.Background(), validAddress, token, testQueryOptions)
		assert.NotNil(t, tokenData)
		assert.Nil(t, err)
		assert.Equal(t, responseTokenData, tokenData)
		assert.False(t, responseTokenData == tokenData) // pointer testing
	})
}

func TestElrondProxy_GetNFTTokenData(t *testing.T) {
	t.Parallel()

	token := "TKN-001122"
	nonce := uint64(37)
	expectedErr := errors.New("expected error")
	validAddress := data.NewAddressFromBytes(bytes.Repeat([]byte("1"), 32))
	emptyQueryOptions := api.AccountQueryOptions{}
	t.Run("nil address, should error", func(t *testing.T) {
		t.Parallel()

		httpClient := createMockClientRespondingBytes(make([]byte, 0))
		args := createMockArgsProxy(httpClient)
		ep, _ := NewProxy(args)

		tokenData, err := ep.GetNFTTokenData(context.Background(), nil, token, nonce, emptyQueryOptions)
		assert.Nil(t, tokenData)
		assert.Equal(t, ErrNilAddress, err)
	})
	t.Run("invalid address, should error", func(t *testing.T) {
		t.Parallel()

		httpClient := createMockClientRespondingBytes(make([]byte, 0))
		args := createMockArgsProxy(httpClient)
		ep, _ := NewProxy(args)

		address := data.NewAddressFromBytes([]byte("invalid"))
		tokenData, err := ep.GetNFTTokenData(context.Background(), address, token, nonce, emptyQueryOptions)
		assert.Nil(t, tokenData)
		assert.Equal(t, ErrInvalidAddress, err)
	})
	t.Run("http client errors, should error", func(t *testing.T) {
		t.Parallel()

		httpClient := createMockClientRespondingError(expectedErr)
		args := createMockArgsProxy(httpClient)
		ep, _ := NewProxy(args)

		tokenData, err := ep.GetNFTTokenData(context.Background(), validAddress, token, nonce, emptyQueryOptions)
		assert.Nil(t, tokenData)
		assert.ErrorIs(t, err, expectedErr)
	})
	t.Run("invalid status, should error", func(t *testing.T) {
		t.Parallel()

		httpClient := createMockClientRespondingBytesWithStatus(make([]byte, 0), http.StatusNotFound)
		args := createMockArgsProxy(httpClient)
		ep, _ := NewProxy(args)

		tokenData, err := ep.GetNFTTokenData(context.Background(), validAddress, token, nonce, emptyQueryOptions)
		assert.Nil(t, tokenData)
		assert.ErrorIs(t, err, ErrHTTPStatusCodeIsNotOK)
	})
	t.Run("invalid response bytes, should error", func(t *testing.T) {
		t.Parallel()

		httpClient := createMockClientRespondingBytes([]byte("invalid json"))
		args := createMockArgsProxy(httpClient)
		ep, _ := NewProxy(args)

		tokenData, err := ep.GetNFTTokenData(context.Background(), validAddress, token, nonce, emptyQueryOptions)
		assert.Nil(t, tokenData)
		assert.NotNil(t, err)
	})
	t.Run("response returned error, should error", func(t *testing.T) {
		t.Parallel()

		response := &data.ESDTNFTResponse{
			Error: expectedErr.Error(),
		}
		responseBytes, _ := json.Marshal(response)

		httpClient := createMockClientRespondingBytes(responseBytes)
		args := createMockArgsProxy(httpClient)
		ep, _ := NewProxy(args)

		tokenData, err := ep.GetNFTTokenData(context.Background(), validAddress, token, nonce, emptyQueryOptions)
		assert.Nil(t, tokenData)
		assert.NotNil(t, err)
		assert.Equal(t, expectedErr.Error(), err.Error())
	})
	t.Run("should work", func(t *testing.T) {
		t.Parallel()

		responseTokenData := &data.ESDTNFTTokenData{
			TokenIdentifier: "identifier",
			Balance:         "balance",
			Properties:      "properties",
			Name:            "name",
			Nonce:           nonce,
			Creator:         "creator",
			Royalties:       "royalties",
			Hash:            []byte("hash"),
			URIs:            [][]byte{[]byte("uri1"), []byte("uri2")},
			Attributes:      []byte("attributes"),
		}
		response := &data.ESDTNFTResponse{
			Data: struct {
				TokenData *data.ESDTNFTTokenData `json:"tokenData"`
			}{
				TokenData: responseTokenData,
			},
		}
		responseBytes, _ := json.Marshal(response)

		httpClient := createMockClientRespondingBytes(responseBytes)
		args := createMockArgsProxy(httpClient)
		ep, _ := NewProxy(args)

		tokenData, err := ep.GetNFTTokenData(context.Background(), validAddress, token, nonce, emptyQueryOptions)
		assert.NotNil(t, tokenData)
		assert.Nil(t, err)
		assert.Equal(t, responseTokenData, tokenData)
		assert.False(t, responseTokenData == tokenData) // pointer testing
	})
	t.Run("should work with query options", func(t *testing.T) {
		t.Parallel()

		responseTokenData := &data.ESDTNFTTokenData{
			TokenIdentifier: "identifier",
			Balance:         "balance",
			Properties:      "properties",
			Name:            "name",
			Nonce:           nonce,
			Creator:         "creator",
			Royalties:       "royalties",
			Hash:            []byte("hash"),
			URIs:            [][]byte{[]byte("uri1"), []byte("uri2")},
			Attributes:      []byte("attributes"),
		}
		response := &data.ESDTNFTResponse{
			Data: struct {
				TokenData *data.ESDTNFTTokenData `json:"tokenData"`
			}{
				TokenData: responseTokenData,
			},
		}
		responseBytes, _ := json.Marshal(response)
		expectedSuffix := "?blockHash=626c6f636b2068617368&blockNonce=3838&blockRootHash=626c6f636b20726f6f742068617368&hintEpoch=3939&onFinalBlock=true&onStartOfEpoch=3737"

		httpClient := &mockHTTPClient{
			doCalled: func(req *http.Request) (*http.Response, error) {
				assert.True(t, strings.HasSuffix(req.URL.String(), expectedSuffix))

				return &http.Response{
					Body:       ioutil.NopCloser(bytes.NewReader(responseBytes)),
					StatusCode: http.StatusOK,
				}, nil
			},
		}
		args := createMockArgsProxy(httpClient)
		ep, _ := NewProxy(args)

		tokenData, err := ep.GetNFTTokenData(context.Background(), validAddress, token, nonce, testQueryOptions)
		assert.NotNil(t, tokenData)
		assert.Nil(t, err)
		assert.Equal(t, responseTokenData, tokenData)
		assert.False(t, responseTokenData == tokenData) // pointer testing
	})
}
