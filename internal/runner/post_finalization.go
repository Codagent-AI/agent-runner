package runner

import "sync"

var postFinalizationHooks struct {
	sync.RWMutex
	hook PostFinalizationHook
}

// SetDefaultPostFinalizationHook installs the process-wide hook used by runs
// whose Options do not provide one. Integrations call this during startup;
// the default remains inert in ordinary builds.
func SetDefaultPostFinalizationHook(hook PostFinalizationHook) {
	postFinalizationHooks.Lock()
	defer postFinalizationHooks.Unlock()
	postFinalizationHooks.hook = hook
}

func defaultPostFinalizationHook() PostFinalizationHook {
	postFinalizationHooks.RLock()
	defer postFinalizationHooks.RUnlock()
	return postFinalizationHooks.hook
}
