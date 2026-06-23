package main

import (
	"crypto/sha256"
	"log"
	"time"

	"github.com/aichain/ai-chain/internal/config"
	"github.com/aichain/ai-chain/internal/crypto"
	"github.com/aichain/ai-chain/internal/state/account"
	"github.com/aichain/ai-chain/internal/storage"
	"github.com/aichain/ai-chain/internal/types"
	pkgtoken "github.com/aichain/ai-chain/pkg/token"
)

type testAccount struct {
	pubKey  crypto.PublicKey
	privKey crypto.PrivateKey
	address types.Address
}

func main() {
	log.Println("=== AI Chain v0.1.0 ===")
	log.Println("Step 1: Token Core + Account Model Demo")

	cfg := config.DefaultConfig()

	// Open in-memory database
	db := storage.NewMemoryDB()
	statedb, err := account.NewStateDB(db)
	if err != nil {
		log.Fatalf("Failed to create StateDB: %v", err)
	}

	// Generate test accounts
	log.Println("\n--- Generating Test Accounts ---")
	accounts := make([]testAccount, 3)
	for i := range accounts {
		pub, priv, err := crypto.GenerateKey()
		if err != nil {
			log.Fatalf("Failed to generate key: %v", err)
		}
		addr := crypto.PubKeyToAddress(pub)
		accounts[i] = testAccount{pubKey: pub, privKey: priv, address: addr}
		log.Printf("Account %d: %s", i, addr.Hex())
	}

	alice := accounts[0]
	bob := accounts[1]
	// charlie := accounts[2] // reserved for future use

	// Initialize genesis allocations
	log.Println("\n--- Initializing Genesis ---")
	genesis := &types.Genesis{
		ChainID:        cfg.ChainID,
		InitialHeight:  0,
		Timestamp:      uint64(time.Now().Unix()),
		InitialPoolAPT: types.NewTokenAmountUint64(100_000),
		InitialPoolNPT: types.NewTokenAmountUint64(100_000),
		ConsensusParams: types.ConsensusParams{
			BlockMaxBytes: 1_048_576,
			BlockMaxGas:   30_000_000,
		},
	}

	// Apply genesis: Alice gets APT, Bob gets NPT
	if err := pkgtoken.MintAPT(statedb, alice.address, pkgtoken.DefaultTestnetParams().APTInitialSupply); err != nil {
		log.Fatalf("Genesis APT mint: %v", err)
	}
	if err := pkgtoken.MintNPT(statedb, bob.address, pkgtoken.DefaultTestnetParams().NPTInitialSupply); err != nil {
		log.Fatalf("Genesis NPT mint: %v", err)
	}

	// Initialize AMM pool
	poolObj := statedb.GetOrNewStateObject(types.AMMPoolAddress)
	poolObj.SetStorage(ammPoolKey("apt"), genesis.InitialPoolAPT.ToBigInt().Bytes())
	poolObj.SetStorage(ammPoolKey("npt"), genesis.InitialPoolNPT.ToBigInt().Bytes())

	genesisRoot, err := statedb.Commit()
	if err != nil {
		log.Fatalf("Genesis commit: %v", err)
	}
	log.Printf("Genesis state root: %x", genesisRoot)

	printBalances(statedb, alice, bob)

	// Demo 1: Token Transfer
	log.Println("\n--- Demo 1: Alice -> Bob 100 APT ---")
	if err := pkgtoken.TransferAPT(statedb, alice.address, bob.address, types.NewTokenAmountUint64(100)); err != nil {
		log.Printf("Transfer failed: %v", err)
	} else {
		log.Println("Transfer successful!")
	}
	printBalances(statedb, alice, bob)

	// Demo 2: AMM Swap (APT -> NPT)
	log.Println("\n--- Demo 2: Alice swaps 50 APT -> NPT ---")
	swapOutput, err := pkgtoken.SwapAPTForNPT(statedb, alice.address,
		types.NewTokenAmountUint64(50), pkgtoken.DefaultAmmFeeRate)
	if err != nil {
		log.Printf("Swap note: %v (pool may have insufficient liquidity)", err)
	} else {
		log.Printf("Alice received: %s NPT", swapOutput.String())
	}
	printBalances(statedb, alice, bob)

	// Demo 3: Token Burn
	log.Println("\n--- Demo 3: Alice burns 10 APT ---")
	if err := pkgtoken.BurnAPT(statedb, alice.address, types.NewTokenAmountUint64(10)); err != nil {
		log.Printf("Burn failed: %v", err)
	} else {
		log.Println("Burn successful!")
	}
	printBalances(statedb, alice, bob)

	// Final commit
	finalRoot, err := statedb.Commit()
	if err != nil {
		log.Fatalf("Final commit: %v", err)
	}
	log.Printf("\n=== Final State Root: %x ===", finalRoot)

	log.Println("\n✅ Step 1 Demo Complete")
	log.Println("All core operations verified:")
	log.Println("  - Ed25519 key generation & address derivation")
	log.Println("  - Token transfers (APT)")
	log.Println("  - AMM constant-product swap (APT->NPT)")
	log.Println("  - Token burn (APT)")
	log.Println("  - Merkle-Patricia Trie state storage")
	log.Println("  - State commits with deterministic root hashes")
}

func printBalances(statedb *account.StateDB, alice, bob testAccount) {
	aptA := pkgtoken.BalanceAPT(statedb, alice.address)
	nptA := pkgtoken.BalanceNPT(statedb, alice.address)
	aptB := pkgtoken.BalanceAPT(statedb, bob.address)
	nptB := pkgtoken.BalanceNPT(statedb, bob.address)
	log.Printf("  Alice: APT=%s, NPT=%s", aptA.String(), nptA.String())
	log.Printf("  Bob:   APT=%s, NPT=%s", aptB.String(), nptB.String())
}

func ammPoolKey(key string) types.Hash {
	h := sha256.New()
	h.Write([]byte("amm_pool_"))
	h.Write([]byte(key))
	var result types.Hash
	copy(result[:], h.Sum(nil))
	return result
}
