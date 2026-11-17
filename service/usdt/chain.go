// Package usdt verifies USDT deposits on chain and derives users' deposit
// addresses. It never holds keys: deposits land in CREATE2 wallets of a
// DepositFactory contract that can only forward funds to the operator's
// destination.
package usdt

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/QuantumNous/new-api/setting/operation_setting"
	"golang.org/x/crypto/sha3"
)

type Kind string

const (
	KindEVM  Kind = "evm"
	KindTron Kind = "tron"
)

// Chain is the fixed on-chain identity of a supported network. A wrong value
// here credits the wrong asset, so it is code, not configuration.
type Chain struct {
	ID       string
	Name     string
	Kind     Kind
	ChainID  uint64
	USDT     [20]byte
	Decimals int32
	// Create2Prefix is 0xff on EVM chains and 0x41 on TRON.
	Create2Prefix byte
	TxURL         string
	AddressURL    string
}

var chains = map[string]Chain{
	operation_setting.UsdtChainBSC: {
		ID: operation_setting.UsdtChainBSC, Name: "BSC (BEP20)", Kind: KindEVM, ChainID: 56,
		USDT: mustHex20("0x55d398326f99059ff775485246999027b3197955"), Decimals: 18, Create2Prefix: 0xff,
		TxURL: "https://bscscan.com/tx/", AddressURL: "https://bscscan.com/address/",
	},
	operation_setting.UsdtChainArbitrum: {
		ID: operation_setting.UsdtChainArbitrum, Name: "Arbitrum One (USDT0)", Kind: KindEVM, ChainID: 42161,
		USDT: mustHex20("0xfd086bc7cd5c481dcc9c85ebe478a1c0b69fcbb9"), Decimals: 6, Create2Prefix: 0xff,
		TxURL: "https://arbiscan.io/tx/", AddressURL: "https://arbiscan.io/address/",
	},
	operation_setting.UsdtChainPolygon: {
		ID: operation_setting.UsdtChainPolygon, Name: "Polygon (PoS)", Kind: KindEVM, ChainID: 137,
		USDT: mustHex20("0xc2132d05d31c914a87c6611c10748aeb04b58e8f"), Decimals: 6, Create2Prefix: 0xff,
		TxURL: "https://polygonscan.com/tx/", AddressURL: "https://polygonscan.com/address/",
	},
	operation_setting.UsdtChainTron: {
		// TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t
		ID: operation_setting.UsdtChainTron, Name: "TRON (TRC20)", Kind: KindTron, ChainID: 728126428,
		USDT: mustHex20("0xa614f803b6fd780986a42c78ec9c7f77e6ded13c"), Decimals: 6, Create2Prefix: 0x41,
		TxURL: "https://tronscan.org/#/transaction/", AddressURL: "https://tronscan.org/#/address/",
	},
}

func LookupChain(id string) (Chain, bool) {
	c, ok := chains[strings.ToLower(strings.TrimSpace(id))]
	return c, ok
}

// transferTopic is keccak256("Transfer(address,address,uint256)").
var transferTopic = mustHex32("0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef")

func keccak256(parts ...[]byte) []byte {
	h := sha3.NewLegacyKeccak256()
	for _, p := range parts {
		h.Write(p)
	}
	return h.Sum(nil)
}

// Salt is keccak256(secret ‖ uint256(userID)); DepositFactory uses it as the
// CREATE2 salt of the user's wallet.
func Salt(secret [32]byte, userID int) [32]byte {
	id := make([]byte, 32)
	new(big.Int).SetInt64(int64(userID)).FillBytes(id)
	var out [32]byte
	copy(out[:], keccak256(secret[:], id))
	return out
}

// WalletAddress mirrors DepositFactory.walletAddress: CREATE2 over an EIP-1167
// clone of implementation.
func WalletAddress(prefix byte, factory, implementation [20]byte, salt [32]byte) [20]byte {
	code := make([]byte, 0, 55)
	code = append(code, mustHex("3d602d80600a3d3981f3363d3d373d3d3d363d73")...)
	code = append(code, implementation[:]...)
	code = append(code, mustHex("5af43d82803e903d91602b57fd5bf3")...)
	digest := keccak256([]byte{prefix}, factory[:], salt[:], keccak256(code))
	var out [20]byte
	copy(out[:], digest[12:])
	return out
}

func ParseHex20(s string) ([20]byte, error) {
	var out [20]byte
	raw, ok := strings.CutPrefix(strings.ToLower(strings.TrimSpace(s)), "0x")
	if !ok || len(raw) != 40 {
		return out, fmt.Errorf("invalid address %q", s)
	}
	b, err := hex.DecodeString(raw)
	if err != nil {
		return out, fmt.Errorf("invalid address %q", s)
	}
	copy(out[:], b)
	return out, nil
}

func ParseHex32(s string) ([32]byte, error) {
	var out [32]byte
	raw, ok := strings.CutPrefix(strings.ToLower(strings.TrimSpace(s)), "0x")
	if !ok || len(raw) != 64 {
		return out, fmt.Errorf("invalid 32-byte value %q", s)
	}
	b, err := hex.DecodeString(raw)
	if err != nil {
		return out, fmt.Errorf("invalid 32-byte value %q", s)
	}
	copy(out[:], b)
	return out, nil
}

// ParseAddress accepts 0x hex on every chain and T... base58 on TRON.
func ParseAddress(c Chain, s string) ([20]byte, error) {
	s = strings.TrimSpace(s)
	if c.Kind == KindTron && strings.HasPrefix(s, "T") {
		return ParseTronAddress(s)
	}
	return ParseHex20(s)
}

func Hex20(a [20]byte) string { return "0x" + hex.EncodeToString(a[:]) }
func Hex32(a [32]byte) string { return "0x" + hex.EncodeToString(a[:]) }

// DisplayAddress renders an address the way wallets on that chain expect it.
func DisplayAddress(c Chain, a [20]byte) string {
	if c.Kind == KindTron {
		return TronAddress(a)
	}
	lower := hex.EncodeToString(a[:])
	hash := keccak256([]byte(lower))
	out := []byte(lower)
	for i := range out {
		if out[i] < 'a' || out[i] > 'f' {
			continue
		}
		nibble := hash[i/2]
		if i%2 == 0 {
			nibble >>= 4
		}
		if nibble&0x0f >= 8 {
			out[i] -= 'a' - 'A'
		}
	}
	return "0x" + string(out)
}

const base58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

// TronAddress renders a 20-byte address as a TRON base58check address.
func TronAddress(a [20]byte) string {
	payload := append([]byte{0x41}, a[:]...)
	first := sha256.Sum256(payload)
	second := sha256.Sum256(first[:])
	full := append(append([]byte{}, payload...), second[:4]...)
	n := new(big.Int).SetBytes(full)
	base, mod := big.NewInt(58), new(big.Int)
	var out []byte
	for n.Sign() > 0 {
		n.DivMod(n, base, mod)
		out = append(out, base58Alphabet[mod.Int64()])
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return string(out)
}

func ParseTronAddress(s string) ([20]byte, error) {
	var out [20]byte
	n := new(big.Int)
	for _, r := range strings.TrimSpace(s) {
		idx := strings.IndexRune(base58Alphabet, r)
		if idx < 0 {
			return out, errors.New("invalid TRON address")
		}
		n.Mul(n, big.NewInt(58))
		n.Add(n, big.NewInt(int64(idx)))
	}
	full := n.Bytes()
	if len(full) != 25 || full[0] != 0x41 {
		return out, errors.New("invalid TRON address")
	}
	first := sha256.Sum256(full[:21])
	second := sha256.Sum256(first[:])
	if string(second[:4]) != string(full[21:]) {
		return out, errors.New("invalid TRON address checksum")
	}
	copy(out[:], full[1:21])
	return out, nil
}

func mustHex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}

func mustHex20(s string) [20]byte {
	a, err := ParseHex20(s)
	if err != nil {
		panic(err)
	}
	return a
}

func mustHex32(s string) [32]byte {
	a, err := ParseHex32(s)
	if err != nil {
		panic(err)
	}
	return a
}
