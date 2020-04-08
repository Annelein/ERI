package main

import (
	"context"
	"sync"

	"github.com/Dynom/ERI/cmd/web/pubsub"
	"github.com/Dynom/ERI/cmd/web/pubsub/gcp"
	"github.com/Dynom/ERI/validator/validations"

	"github.com/Dynom/ERI/cmd/web/erihttp/handlers"
	"github.com/Dynom/ERI/cmd/web/hitlist"
	"github.com/Dynom/ERI/types"
	"github.com/Dynom/ERI/validator"
	"github.com/Dynom/TySug/finder"
	"github.com/sirupsen/logrus"
)

// validatorHitListProxy Keeps HitList up-to-date and acts as a partial cache for the validator
func validatorHitListProxy(hitList *hitlist.HitList, logger logrus.FieldLogger, fn validator.CheckFn) validator.CheckFn {

	logger = logger.WithField("middleware", "cache_proxy")
	return func(ctx context.Context, parts types.EmailParts, options ...validator.ArtifactFn) validator.Result {
		var afn = options

		cvr, exists := hitList.GetDomainValidationResult(hitlist.Domain(parts.Domain))

		logger := logger.WithFields(logrus.Fields{
			handlers.RequestID.String(): ctx.Value(handlers.RequestID),
			"cache_hit":                 exists,
		})

		if exists {
			afn = append(afn, func(artifact *validator.Artifact) {

				// The cache allows us to skip expensive steps that we might be doing. However basic syntax validation should
				// always be done. We're discriminating on domain, so we can't vouch for the entire address without a basic test
				artifact.Steps = cvr.Steps.RemoveFlag(validations.FSyntax)
				artifact.Validations = cvr.Validations.RemoveFlag(validations.FSyntax)
			})
		}

		vr := fn(ctx, parts, afn...)

		err := hitList.Add(parts, vr)
		if err != nil {
			logger.WithFields(logrus.Fields{
				"error": err,
				"parts": parts,
			}).Error("HitList rejected value")
		}

		return vr
	}
}

// validatorPersistProxy persist the result of the validator.
func validatorPersistProxy(persist *sync.Map, hitList *hitlist.HitList, logger logrus.FieldLogger, fn validator.CheckFn) validator.CheckFn {
	logger = logger.WithField("middleware", "persist_proxy")
	return func(ctx context.Context, parts types.EmailParts, options ...validator.ArtifactFn) validator.Result {

		log := logger.WithField(handlers.RequestID.String(), ctx.Value(handlers.RequestID))

		_, existed := hitList.Has(parts)

		vr := fn(ctx, parts, options...)

		if !existed && vr.HasValidStructure() {
			persist.Store(parts.Address, vr)
			log.WithFields(logrus.Fields{
				"email":       parts.Address,
				"steps":       vr.Steps.String(),
				"validations": vr.Validations.String(),
			}).Debug("Persisted result")
		}

		return vr
	}
}

func validatorNotifyProxy(svc *gcp.PubSubSvc, _ *hitlist.HitList, logger logrus.FieldLogger, fn validator.CheckFn) validator.CheckFn {

	logger = logger.WithField("middleware", "notification_publisher")
	return func(ctx context.Context, parts types.EmailParts, options ...validator.ArtifactFn) validator.Result {
		log := logger.WithField(handlers.RequestID.String(), ctx.Value(handlers.RequestID))

		vr := fn(ctx, parts, options...)

		data := pubsub.Data{
			Local:       parts.Local,
			Domain:      parts.Domain,
			Validations: vr.Validations,
			Steps:       vr.Steps,
		}

		err := svc.Publish(ctx, data)

		if err != nil {
			log.WithFields(logrus.Fields{
				"error": err,
				"data":  data,
			}).Error("Publishing failed")
		}

		return vr
	}
}

// validatorUpdateFinderProxy updates Finder whenever a new and good domain has been discovered
func validatorUpdateFinderProxy(finder *finder.Finder, hitList *hitlist.HitList, logger logrus.FieldLogger, fn validator.CheckFn) validator.CheckFn {
	log := logger.WithField("middleware", "finder_updater")
	return func(ctx context.Context, parts types.EmailParts, options ...validator.ArtifactFn) validator.Result {

		log := log.WithField(handlers.RequestID.String(), ctx.Value(handlers.RequestID))

		vr := fn(ctx, parts, options...)

		if vr.Validations.IsValidationsForValidDomain() && !finder.Exact(parts.Domain) {
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
