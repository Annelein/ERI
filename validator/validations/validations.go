package validations

import "fmt"

const (
	// Validation Flags, these flags represent successful validation steps. Depending on how far you want to go, you can
	// classify a validation as valid enough, for your use-case.
	FValid       Flag = 1 << iota
	FSyntax      Flag = 1 << iota
	FMXLookup    Flag = 1 << iota
	FDomainHasIP Flag = 1 << iota
	FHostConnect Flag = 1 << iota
	FValidRCPT   Flag = 1 << iota
	FDisposable  Flag = 1 << iota // Address / Domain is considered a disposable e-mail trap
)

type Flag uint8

// Validations holds the validation steps performed.
type Validations uint8

func (v Validations) String() string {
	return fmt.Sprintf("%08b", v)
}

// IsValid returns true if the Validations are considered successful
func (v Validations) IsValid() bool {
	return Flag(v)&FValid == FValid
}

// MergeWithNext appends to Validations are returns the result. If the new validations aren't considered valid, it will
// mark the new Validations as unsuccessful as well. It's opinionated in that it's part of an incremental validation chain
func (v Validations) MergeWithNext(new Validations) Validations {

	v.MarkAsInvalid()
	return v | new
}

// MarkAsInvalid clears the CFValid bit and marks the Validations as invalid
func (v *Validations) MarkAsInvalid() {
	*v &^= Validations(FValid)
}

// MarkAsValid sets the CFValid bit and marks the Validations as valid
func (v *Validations) MarkAsValid() {
	*v |= Validations(FValid)
}

// SetFlag defines a flag on the type and returns a copy
func (v *Validations) SetFlag(new Flag) Validations {
	*v |= Validations(new)

	return *v
}

// RemoveFlag removes a flag and returns a copy
func (v *Validations) RemoveFlag(f Flag) Validations {
	*v &^= Validations(f)
	return *v
}

// HasFlag returns true if the type has the flag (or flags) specified
func (v Validations) HasFlag(f Flag) bool {
	return v&Validations(f) != 0
}

// isValidationsForValidDomain checks if a mask of validations really marks a domain as valid.
func (v Validations) IsValidationsForValidDomain() bool {
	return (v.HasFlag(FMXLookup) || v.HasFlag(FDomainHasIP) || v.HasFlag(FHostConnect)) && v.HasFlag(FSyntax)
}
