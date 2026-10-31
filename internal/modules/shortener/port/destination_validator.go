package port

type DestinationValidator interface {
	Validate(destination string) error
}
