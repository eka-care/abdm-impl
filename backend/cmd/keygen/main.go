package main

import (
	"fmt"

	abdmecdh "github.com/eka-care/abdm-ecdh/go"
)

func main() {
	km, err := abdmecdh.New().GenerateKeyMaterial()
	if err != nil {
		panic(err)
	}
	fmt.Println("# Paste into .env.local if you want static keys (dev only)")
	fmt.Printf("HIP_PRIVATE_KEY=%s\n", km.PrivateKey)
	fmt.Printf("HIP_PUBLIC_KEY=%s\n", km.PublicKey)
	fmt.Printf("HIP_X509_PUBLIC_KEY=%s\n", km.X509PublicKey)
	fmt.Printf("HIP_NONCE=%s\n", km.Nonce)
}
