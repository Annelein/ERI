package main

import (
	"context"
	"sync"

	"cloud.google.com/go/pubsub"

	"github.com/Dynom/ERI/cmd/web/erihttp/handlers"
	"github.com/Dynom/ERI/cmd/web/hitlist"
	"github.com/Dynom/ERI/types"
	"github.com/Dynom/ERI/validator"
	"github.com/Dynom/TySug/finder"
	"github.com/sirupsen/logrus"
)

// validatorCacheProxy caches expensive validation steps for domains only
func validatorCacheProxy(cache *sync.Map, logger logrus.FieldLogger, fn validator.CheckFn) validator.CheckFn {
	log := logger.WithField("middleware", "cache_proxy")
	return func(ctx context.Context, parts types.EmailParts, options ...validator.ArtifactFn) validator.Result {
		var vr validator.Result
		var afn = options

		log := log.WithField(handlers.RequestID.String(), ctx.Value(handlers.RequestID))

		cvr, cacheHit := cache.Load(parts.Domain)
		if cacheHit {
			if tvr, ok := cvr.(validator.Result); ok {
				afn = append(afn, func(artifact *validator.Artifact) {

					// The cache allows us to skip expensive steps that we might be doing. However basic syntax validation should
					// always be done. We're discriminating on domain, so we can't vouch for the entire address without a basic test
					artifact.Steps = tvr.Steps
					artifact.Validations = tvr.Validations
				})
			}
		}

		vr = fn(ctx, parts, afn...)

		if vr.HasValidStructure() {
			cache.Store(parts.Domain, vr)
			log.WithFields(logrus.Fields{
				"email":       parts.Address,
				"validations": vr.Validations.String(),
				"steps":       vr.Steps.String(),
				"cache_hit":   cacheHit,
			}).Debug("Cache proxy result")
		}

		return vr
	}
}

// validatorPersistProxy persist the result of the validator.
func validatorPersistProxy(persist *sync.Map, logger logrus.FieldLogger, fn validator.CheckFn) validator.CheckFn {
	log := logger.WithField("middleware", "persist_proxy")
	return func(ctx context.Context, parts types.EmailParts, options ...validator.ArtifactFn) validator.Result {

		log := log.WithField(handlers.RequestID.String(), ctx.Value(handlers.RequestID))
		vr := fn(ctx, parts, options...)

		if vr.HasValidStructure() {
			persist.Store(parts.Domain, vr)
			log.WithFields(logrus.Fields{
				"email":       parts.Address,
				"steps":       vr.Steps.String(),
				"validations": vr.Validations.String(),
			}).Debug("Persisted result")
		}

		return vr
	}
}

func validatorNotifyProxy(topic *pubsub.Topic, hitList *hitlist.HitList, logger logrus.FieldLogger, fn validator.CheckFn) validator.CheckFn {
	log := logger.WithField("middleware", "notification_publisher")
	return func(ctx context.Context, parts types.EmailParts, options ...validator.ArtifactFn) validator.Result {
		log := log.WithField(handlers.RequestID.String(), ctx.Value(handlers.RequestID))
		vr := fn(ctx, parts, options...)

		//_, exists := hitList.GetDomainValidationResult(hitlist.Domain(parts.Domain))
		//if exists {
		//	return vr
		//}

		log.WithFields(logrus.Fields{
			"topic": topic.String(),
		}).Debug("Publishing result")

		pr := topic.Publish(ctx, &pubsub.Message{
			Data: []byte("ZOMG THE DATA"),
			Attributes: map[string]string{
				"eri_version": Version,
			},
		})

		<-pr.Ready()

		sid, err := pr.Get(ctx)
		if err != nil {
			log.WithError(err).Errorf("Error reading Publish Result")
			return vr
		}

		log.WithField("server_id", sid).Debug("Done publishing")

		toNotify := struct {
			Channel          string
			Local            string
			Domain           string
			ValidationResult validator.Result
		}{
			Channel:          "foo",
			Local:            parts.Local,
			Domain:           parts.Domain,
			ValidationResult: vr,
		}

		_ = toNotify
		_ = log
		_ = topic

		return vr
	}
}

// validatorUpdateFinderProxy updates Finder whenever a new and good domain has been discovered
func validatorUpdateFinderProxy(finder *finder.Finder, hitList *hitlist.HitList, logger logrus.FieldLogger, fn validator.CheckFn) validator.CheckFn {
	log := logger.WithField("middleware", "finder_updater")
	return func(ctx context.Context, parts types.EmailParts, options ...validator.ArtifactFn) validator.Result {
		var err error

		log := log.WithField(handlers.RequestID.String(), ctx.Value(handlers.RequestID))

		vr := fn(ctx, parts, options...)

		if parts.Local != "" {
			err = hitList.AddEmailAddress(parts.Address, vr)
		} else {
			err = hitList.AddDomain(parts.Domain, vr)
		}

		if err != nil {
			log.WithError(err).Error("HitList rejected value")
			return vr
		}

		if vr.Validations.IsValid() && !finder.Exact(parts.Domain) {

			finder.Refresh(hitList.GetValidAndUsageSortedDomains())

			log.WithFields(logrus.Fields{
				"email":       parts.Address,
				"steps":       vr.Steps.String(),
				"validations": vr.Validations.String(),
			}).Debug("Updated Finder")
		}

		return vr
	}
}
