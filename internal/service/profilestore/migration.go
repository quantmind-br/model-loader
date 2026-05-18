package profilestore

import "github.com/quantmind-br/model-loader/internal/domain"

func MigrateProfile(p *domain.Profile) {
	switch p.SchemaVersion {
	case 0, 1:
		migrateV1ToV2(p)
		fallthrough
	case 2:
		migrateV2ToV3(p)
	}
}

func migrateV1ToV2(p *domain.Profile) {
	p.SchemaVersion = 2
}

func migrateV2ToV3(p *domain.Profile) {
	if p.Launch.RestartPolicy == "" {
		p.Launch.RestartPolicy = domain.RestartPolicyNone
	}
	if p.Launch.MaxRestarts == 0 {
		p.Launch.MaxRestarts = 3
	}
	if p.Launch.BackoffSeconds == 0 {
		p.Launch.BackoffSeconds = 5
	}
	p.SchemaVersion = 3
}
