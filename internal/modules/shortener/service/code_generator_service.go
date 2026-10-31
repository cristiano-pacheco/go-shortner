package service

import (
	"crypto/rand"
	"math/big"

	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/port"
)

const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

type CodeGeneratorService struct{}

var _ port.CodeGenerator = (*CodeGeneratorService)(nil)

func NewCodeGeneratorService() *CodeGeneratorService { return &CodeGeneratorService{} }

func (g *CodeGeneratorService) Generate() (string, error) {
	code := make([]byte, 8)
	for i := range code {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			return "", err
		}
		code[i] = alphabet[n.Int64()]
	}
	return string(code), nil
}
