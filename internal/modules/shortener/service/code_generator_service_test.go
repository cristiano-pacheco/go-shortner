package service_test

import (
	"bytes"
	"crypto/rand"
	"errors"
	"io"
	"testing"
	"testing/iotest"

	"github.com/stretchr/testify/suite"

	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/service"
)

type CodeGeneratorServiceSuite struct {
	suite.Suite

	generator *service.CodeGeneratorService
}

//nolint:paralleltest // Tests temporarily replace the process-wide random source.
func TestCodeGeneratorServiceSuite(t *testing.T) {
	suite.Run(t, new(CodeGeneratorServiceSuite))
}

func (s *CodeGeneratorServiceSuite) SetupTest() {
	s.generator = service.NewCodeGeneratorService()
}

func (s *CodeGeneratorServiceSuite) TestGenerateProducesEightAlphanumericCharacters() {
	code, err := s.generator.Generate()

	s.Require().NoError(err)
	s.Require().Len(code, 8)
	s.Regexp(`^[A-Za-z0-9]{8}$`, code)
}

func (s *CodeGeneratorServiceSuite) TestGenerateMapsRandomValuesToAlphabet() {
	original := rand.Reader
	//nolint:reassign // Supply deterministic random values.
	rand.Reader = bytes.NewReader([]byte{0, 25, 26, 51, 52, 61, 1, 60})

	s.T().Cleanup(func() {
		rand.Reader = original //nolint:reassign // Restore the process-wide source.
	})

	code, err := s.generator.Generate()

	s.Require().NoError(err)
	s.Equal("AZaz09B8", code)
}

func (s *CodeGeneratorServiceSuite) TestGenerateReturnsErrorWithoutPartialCode() {
	expectedErr := errors.New("random source failed")
	original := rand.Reader
	//nolint:reassign // Simulate a random source failure.
	rand.Reader = io.MultiReader(bytes.NewReader([]byte{0, 1}), iotest.ErrReader(expectedErr))

	s.T().Cleanup(func() {
		rand.Reader = original //nolint:reassign // Restore the process-wide source.
	})

	code, err := s.generator.Generate()

	s.ErrorIs(err, expectedErr) //nolint:testifylint // Check the returned code even when the error differs.
	s.Empty(code)
}
