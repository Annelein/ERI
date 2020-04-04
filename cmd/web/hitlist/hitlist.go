package hitlist

import (
	"bytes"
	"errors"
	"hash"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Dynom/ERI/types"
	"github.com/Dynom/ERI/validator"
)

var (
	ErrInvalidDomainSyntax = errors.New("invalid domain syntax")
)

type Hits map[Domain]Hit
type Domain string
type Hit struct {
	Recipients       []Recipient
	ValidUntil       time.Time
	ValidationResult validator.Result
}

type Recipient []byte

func New(h hash.Hash, ttl time.Duration) *HitList {
	l := HitList{
		hits: make(Hits),
		lock: sync.RWMutex{},
		h:    h,
		ttl:  ttl,
	}

	return &l
}

type HitList struct {
	hits Hits
	ttl  time.Duration
	lock sync.RWMutex
	h    hash.Hash
}

// Has returns true if HitList knows about (part of) the argument
func (hl *HitList) Has(parts types.EmailParts) (domain, local bool) {
	var hit Hit

	hl.lock.RLock()
	defer hl.lock.RUnlock()

	if hit, domain = hl.hits[Domain(parts.Domain)]; domain {
		recipient := Recipient(hl.h.Sum([]byte(parts.Local)))
		for _, v := range hit.Recipients {
			if bytes.Equal(recipient, v) {
				local = true
				return
			}
		}
	}

	return
}

func (hl *HitList) GetDomainValidationResult(d Domain) (validator.Result, bool) {
	hl.lock.RLock()
	hit, ok := hl.hits[d]
	hl.lock.RUnlock()

	if ok {
		return hit.ValidationResult, ok
	}

	return validator.Result{}, ok
}

// GetValidAndUsageSortedDomains returns the used domains, sorted by their associated recipients (high>low)
func (hl *HitList) GetValidAndUsageSortedDomains() []string {
	hl.lock.RLock()
	var domains = getValidDomains(hl.hits)
	hl.lock.RUnlock()

	return domains
}

func (hl *HitList) Add(parts types.EmailParts, vr validator.Result) error {
	if parts.Local == "" {
		return hl.AddDomain(parts.Domain, vr)
	}

	return hl.AddEmailAddress(parts.Address, vr)
}

// AddEmailAddressDeadline Same as AddEmailAddress, but allows for custom TTL. Duration shouldn't be negative.
func (hl *HitList) AddEmailAddressDeadline(email string, vr validator.Result, duration time.Duration) error {
	var domain Domain
	var safeLocal Recipient

	{
		email = strings.ToLower(email)
		parts, err := types.NewEmailParts(email) // @todo prevent multiple calls to types.NewEmailParts()
		if err != nil {
			return err
		}

		if len(parts.Domain) == 0 || len(parts.Local) == 0 {
			return ErrInvalidDomainSyntax
		}

		safeLocal = hl.h.Sum([]byte(parts.Local))
		domain = Domain(parts.Domain)
	}

	hl.lock.Lock()
	defer hl.lock.Unlock()

	var now = time.Now()
	dh, ok := hl.hits[domain]

	if !ok {

		hl.hits[domain] = Hit{
			Recipients:       []Recipient{safeLocal},
			ValidUntil:       now.Add(duration),
			ValidationResult: vr,
		}

		return nil
	}

	dh.ValidationResult.Validations = dh.ValidationResult.Validations.MergeWithNext(vr.Validations)
	dh.ValidationResult.Steps = dh.ValidationResult.Steps.MergeWithNext(vr.Steps)
	dh.ValidUntil = now.Add(duration)
	dh.Recipients = append(dh.Recipients, safeLocal)

	hl.hits[domain] = dh

	return nil
}

// AddEmailAddress records validations for a particular e-mail address. AddEmailAddress clears previously seen
// validators if you want to merge, first fetch, merge and pass the resulting Validations to AddEmailAddress()
func (hl *HitList) AddEmailAddress(email string, vr validator.Result) error {
	return hl.AddEmailAddressDeadline(email, vr, hl.ttl)
}

// AddDomain learns of a domain and it's validity. It overwrites the existing validations, when applicable for a domain
func (hl *HitList) AddDomain(d string, vr validator.Result) error {
	var domain = Domain(strings.ToLower(d))

	if len(domain) == 0 {
		return ErrInvalidDomainSyntax
	}

	hl.lock.Lock()
	defer hl.lock.Unlock()

	hit, ok := hl.hits[domain]
	if !ok {
		hl.hits[domain] = Hit{
			Recipients:       []Recipient{},
			ValidUntil:       time.Now().Add(hl.ttl),
			ValidationResult: vr,
		}

		return nil
	}

	hit.ValidationResult.Validations = hit.ValidationResult.Validations.MergeWithNext(vr.Validations)
	hit.ValidationResult.Steps = hit.ValidationResult.Steps.MergeWithNext(vr.Steps)
	hl.hits[domain] = hit

	return nil
}

// getValidDomains returns domains which are valid, sorted by their recipients in descending order
func getValidDomains(hits Hits) []string {
	type stats struct {
		Domain     string
		Recipients int64
	}

	var sortStats = make([]stats, 0, len(hits))

	var now = time.Now()
	for domain, details := range hits {

		if !details.ValidationResult.Validations.IsValidationsForValidDomain() {
			continue
		}

		if !details.ValidUntil.After(now) {
			continue
		}

		sortStats = append(sortStats, stats{
			Domain:     string(domain),
			Recipients: int64(len(details.Recipients)),
		})
	}

	// Sorting on recipient count in Descending order
	sort.Slice(sortStats, func(i, j int) bool {
		return sortStats[i].Recipients > sortStats[j].Recipients
	})

	// @todo Could probably be an object pool, could relieve the GC
	result := make([]string, 0, len(sortStats))
	for _, stats := range sortStats {
		result = append(result, stats.Domain)
	}

	return result
}
