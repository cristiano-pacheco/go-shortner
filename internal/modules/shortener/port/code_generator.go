package port

// CodeGenerator produces independent public short codes.
type CodeGenerator interface {
	Generate() (string, error)
}
