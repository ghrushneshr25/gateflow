// Package appnexus documents Gateflow's use of github.com/ghrushneshr25/nexus.
//
// Each service binary owns its process-local nexus registry:
//   - Constructors are declared once at wiring time (after config/pool exist).
//   - Successful resolutions are lazy singletons.
//   - Tests call nexus.Reset(), then re-declare mocks/fakes before MustGet.
//
// Domain types must not be shared across services via this package.
// Only technical guidance and optional test helpers live here.
package appnexus
