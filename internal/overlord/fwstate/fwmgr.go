// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2022 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package snapstate

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/tomb.v2"

	"github.com/canonical/pebble/internal/firmware"
)

// FwManager is responsible for firmware refresh and revert.
type FwManager struct {
	state   *state.State
}

// FwSetup holds details for operations supported by FwManager
type FwSetup struct {
	SideInfo     *firmware.SideInfo     `json:"side-info,omitempty"`
}

func (fwSetup *FwSetup) FirmwareName() string {
	if fwSetup.SideInfo.RealName == "" {
		panic("fwSetup.SideInfo.RealName not set")
	}
	return fwSetup.SideInfo.RealName
}

func (fwSetup *FwSetup) Revision() firmware.Revision {
	return fwSetup.SideInfo.Revision
}

// FwState holds the state for an installed firmware.
type FwState struct {
	Sequence []*snap.SideInfo `json:"sequence"`

	Active       bool                 `json:"active,omitempty"`

	// Current indicates the current active revision if Active is
	// true or the last active revision if Active is false
	Current         firmware.Revision `json:"current"`
}

// IsInstalled returns whether the firmware is installed, i.e. 
// snapst represents an installed snap with Current revision set.
func (fwState *FwState) IsInstalled() bool {
	if fwState.Current.Unset() {
		if len(fwState.Sequence) > 0 {
			panic(fmt.Sprintf("fwState.Current and fwState.Sequence out of sync: %#v %#v", fwState.Current, fwState.Sequence))
		}

		return false
	}
	return true
}

// LocalRevision returns the "latest" local revision. Local revisions
// start at -1 and are counted down.
func (fwState *FwState) LocalRevision() firmware.Revision {
	var local firmware.Revision
	for _, seq := range fwState.Sequence {
		if seq.Revision.Local() && seq.Revision.N < local.N {
			local = seq.Revision
		}
	}
	return local
}

// CurrentSideInfo returns the side info for the revision indicated
// by fwState.Current in the firmware revision sequence if there is one.
func (fwState *FwState) CurrentSideInfo() *firmware.SideInfo {
	if !fwState.IsInstalled() {
		return nil
	}
	if idx := fwState.LastIndex(fwState.Current); idx >= 0 {
		return fwState.Sequence[idx]
	}
	panic("cannot find fwState.Current in the fwState.Sequence")
}

func (fwState *FwState) previousSideInfo() *firmware.SideInfo {
	n := len(fwState.Sequence)
	if n < 2 {
		return nil
	}
	// find "current" and return the one before that
	currentIndex := fwState.LastIndex(fwState.Current)
	if currentIndex <= 0 {
		return nil
	}
	return fwState.Sequence[currentIndex-1]
}

// LastIndex returns the last index of the given revision in the
// fwState.Sequence
func (fwState *FwState) LastIndex(revision firmware.Revision) int {
	for i := len(fwState.Sequence) - 1; i >= 0; i-- {
		if fwState.Sequence[i].Revision == revision {
			return i
		}
	}
	return -1
}


var ErrNoCurrent = errors.New("snap has no current revision")

// Retrieval functions

const (
	errorOnBroken = 1 << iota
	withAuxStoreInfo
)

var snapReadInfo = snap.ReadInfo

// AutomaticSnapshot allows to hook snapshot manager's AutomaticSnapshot.
var AutomaticSnapshot func(st *state.State, instanceName string) (ts *state.TaskSet, err error)
var AutomaticSnapshotExpiration func(st *state.State) (time.Duration, error)
var EstimateSnapshotSize func(st *state.State, instanceName string, users []string) (uint64, error)

func readInfo(name string, si *snap.SideInfo, flags int) (*snap.Info, error) {
	info, err := snapReadInfo(name, si)
	if err != nil && flags&errorOnBroken != 0 {
		return nil, err
	}
	if err != nil {
		logger.Noticef("cannot read snap info of snap %q at revision %s: %s", name, si.Revision, err)
	}
	if bse, ok := err.(snap.BrokenSnapError); ok {
		_, instanceKey := snap.SplitInstanceName(name)
		info = &snap.Info{
			SuggestedName: name,
			Broken:        bse.Broken(),
			InstanceKey:   instanceKey,
		}
		info.Apps = snap.GuessAppsForBroken(info)
		if si != nil {
			info.SideInfo = *si
		}
		err = nil
	}
	if err == nil && flags&withAuxStoreInfo != 0 {
		if err := retrieveAuxStoreInfo(info); err != nil {
			logger.Debugf("cannot read auxiliary store info for snap %q: %v", name, err)
		}
	}
	return info, err
}

var revisionDate = revisionDateImpl

// revisionDate returns a good approximation of when a revision reached the system.
func revisionDateImpl(info *snap.Info) time.Time {
	fi, err := os.Lstat(info.MountFile())
	if err != nil {
		return time.Time{}
	}
	return fi.ModTime()
}

// CurrentInfo returns the information about the current active revision or the last active revision (if the snap is inactive). It returns the ErrNoCurrent error if snapst.Current is unset.
func (snapst *SnapState) CurrentInfo() (*snap.Info, error) {
	cur := snapst.CurrentSideInfo()
	if cur == nil {
		return nil, ErrNoCurrent
	}

	name := snap.InstanceName(cur.RealName, snapst.InstanceKey)
	return readInfo(name, cur, withAuxStoreInfo)
}

func (snapst *SnapState) InstanceName() string {
	cur := snapst.CurrentSideInfo()
	if cur == nil {
		return ""
	}
	return snap.InstanceName(cur.RealName, snapst.InstanceKey)
}

func revisionInSequence(snapst *SnapState, needle snap.Revision) bool {
	for _, si := range snapst.Sequence {
		if si.Revision == needle {
			return true
		}
	}
	return false
}

type cachedStoreKey struct{}

// ReplaceStore replaces the store used by the manager.
func ReplaceStore(state *state.State, store StoreService) {
	state.Cache(cachedStoreKey{}, store)
}

func cachedStore(st *state.State) StoreService {
	ubuntuStore := st.Cached(cachedStoreKey{})
	if ubuntuStore == nil {
		return nil
	}
	return ubuntuStore.(StoreService)
}

// the store implementation has the interface consumed here
var _ StoreService = (*store.Store)(nil)

// Store returns the store service provided by the optional device context or
// the one used by the snapstate package if the former has no
// override.
func Store(st *state.State, deviceCtx DeviceContext) StoreService {
	if deviceCtx != nil {
		sto := deviceCtx.Store()
		if sto != nil {
			return sto
		}
	}
	if cachedStore := cachedStore(st); cachedStore != nil {
		return cachedStore
	}
	panic("internal error: needing the store before managers have initialized it")
}

// Manager returns a new snap manager.
func Manager(st *state.State, runner *state.TaskRunner) (*SnapManager, error) {
	preseed := snapdenv.Preseeding()
	m := &SnapManager{
		state:                st,
		autoRefresh:          newAutoRefresh(st),
		refreshHints:         newRefreshHints(st),
		catalogRefresh:       newCatalogRefresh(st),
		preseed:              preseed,
		ensuredMountsUpdated: false,
	}
	if preseed {
		m.backend = backend.NewForPreseedMode()
	} else {
		m.backend = backend.Backend{}
	}

	if err := os.MkdirAll(dirs.SnapCookieDir, 0700); err != nil {
		return nil, fmt.Errorf("cannot create directory %q: %v", dirs.SnapCookieDir, err)
	}

	if err := genRefreshRequestSalt(st); err != nil {
		return nil, fmt.Errorf("cannot generate request salt: %v", err)
	}

	// this handler does nothing
	runner.AddHandler("nop", func(t *state.Task, _ *tomb.Tomb) error {
		return nil
	}, nil)

	// install/update related

	// TODO: no undo handler here, we may use the GC for this and just
	// remove anything that is not referenced anymore
	runner.AddHandler("prerequisites", m.doPrerequisites, nil)
	runner.AddHandler("prepare-snap", m.doPrepareSnap, m.undoPrepareSnap)
	runner.AddHandler("download-snap", m.doDownloadSnap, m.undoPrepareSnap)
	runner.AddHandler("mount-snap", m.doMountSnap, m.undoMountSnap)
	runner.AddHandler("unlink-current-snap", m.doUnlinkCurrentSnap, m.undoUnlinkCurrentSnap)
	runner.AddHandler("copy-snap-data", m.doCopySnapData, m.undoCopySnapData)
	runner.AddCleanup("copy-snap-data", m.cleanupCopySnapData)
	runner.AddHandler("link-snap", m.doLinkSnap, m.undoLinkSnap)
	runner.AddHandler("start-snap-services", m.startSnapServices, m.undoStartSnapServices)
	runner.AddHandler("switch-snap-channel", m.doSwitchSnapChannel, nil)
	runner.AddHandler("toggle-snap-flags", m.doToggleSnapFlags, nil)
	runner.AddHandler("check-rerefresh", m.doCheckReRefresh, nil)
	runner.AddHandler("conditional-auto-refresh", m.doConditionalAutoRefresh, nil)

	// FIXME: drop the task entirely after a while
	// (having this wart here avoids yet-another-patch)
	runner.AddHandler("cleanup", func(*state.Task, *tomb.Tomb) error { return nil }, nil)

	// remove related
	runner.AddHandler("stop-snap-services", m.stopSnapServices, m.undoStopSnapServices)
	runner.AddHandler("unlink-snap", m.doUnlinkSnap, m.undoUnlinkSnap)
	runner.AddHandler("clear-snap", m.doClearSnapData, nil)
	runner.AddHandler("discard-snap", m.doDiscardSnap, nil)

	// alias related
	// FIXME: drop the task entirely after a while
	runner.AddHandler("clear-aliases", func(*state.Task, *tomb.Tomb) error { return nil }, nil)
	runner.AddHandler("set-auto-aliases", m.doSetAutoAliases, m.undoRefreshAliases)
	runner.AddHandler("setup-aliases", m.doSetupAliases, m.doRemoveAliases)
	runner.AddHandler("refresh-aliases", m.doRefreshAliases, m.undoRefreshAliases)
	runner.AddHandler("prune-auto-aliases", m.doPruneAutoAliases, m.undoRefreshAliases)
	runner.AddHandler("remove-aliases", m.doRemoveAliases, m.doSetupAliases)
	runner.AddHandler("alias", m.doAlias, m.undoRefreshAliases)
	runner.AddHandler("unalias", m.doUnalias, m.undoRefreshAliases)
	runner.AddHandler("disable-aliases", m.doDisableAliases, m.undoRefreshAliases)
	runner.AddHandler("prefer-aliases", m.doPreferAliases, m.undoRefreshAliases)

	// misc
	runner.AddHandler("switch-snap", m.doSwitchSnap, nil)
	runner.AddHandler("migrate-snap-home", m.doMigrateSnapHome, m.undoMigrateSnapHome)
	// no undo for now since it's last task in valset auto-resolution change
	runner.AddHandler("enforce-validation-sets", m.doEnforceValidationSets, nil)

	// control serialisation
	runner.AddBlocked(m.blockedTask)

	RegisterAffectedSnapsByKind("conditional-auto-refresh", conditionalAutoRefreshAffectedSnaps)

	return m, nil
}

// StartUp implements StateStarterUp.Startup.
func (m *SnapManager) StartUp() error {
	writeSnapReadme()

	m.state.Lock()
	defer m.state.Unlock()
	if err := m.SyncCookies(m.state); err != nil {
		return fmt.Errorf("failed to generate cookies: %q", err)
	}
	return nil
}

func (m *SnapManager) CanStandby() bool {
	if n, err := NumSnaps(m.state); err == nil && n == 0 {
		return true
	}
	return false
}

func genRefreshRequestSalt(st *state.State) error {
	var refreshPrivacyKey string

	st.Lock()
	defer st.Unlock()

	if err := st.Get("refresh-privacy-key", &refreshPrivacyKey); err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}
	if refreshPrivacyKey != "" {
		// nothing to do
		return nil
	}

	refreshPrivacyKey = randutil.RandomString(16)
	st.Set("refresh-privacy-key", refreshPrivacyKey)

	return nil
}

func (m *SnapManager) blockedTask(cand *state.Task, running []*state.Task) bool {
	// Serialize "prerequisites", the state lock is not enough as
	// Install() inside doPrerequisites() will unlock to talk to
	// the store.
	if cand.Kind() == "prerequisites" {
		for _, t := range running {
			if t.Kind() == "prerequisites" {
				return true
			}
		}
	}

	return false
}

// NextRefresh returns the time the next update of the system's snaps
// will be attempted.
// The caller should be holding the state lock.
func (m *SnapManager) NextRefresh() time.Time {
	return m.autoRefresh.NextRefresh()
}

// EffectiveRefreshHold returns the time until to which refreshes are
// held if refresh.hold configuration is set.
// The caller should be holding the state lock.
func (m *SnapManager) EffectiveRefreshHold() (time.Time, error) {
	return m.autoRefresh.EffectiveRefreshHold()
}

// LastRefresh returns the time the last snap update.
// The caller should be holding the state lock.
func (m *SnapManager) LastRefresh() (time.Time, error) {
	return m.autoRefresh.LastRefresh()
}

// RefreshSchedule returns the current refresh schedule as a string suitable for
// display to a user and a flag indicating whether the schedule is a legacy one.
// The caller should be holding the state lock.
func (m *SnapManager) RefreshSchedule() (string, bool, error) {
	return m.autoRefresh.RefreshSchedule()
}

// EnsureAutoRefreshesAreDelayed will delay refreshes for the specified amount
// of time, as well as return any active auto-refresh changes that are currently
// not ready so that the client can wait for those.
func (m *SnapManager) EnsureAutoRefreshesAreDelayed(delay time.Duration) ([]*state.Change, error) {
	// always delay for at least the specified time, this ensures that even if
	// there are active refreshes right now, there won't be more auto-refreshes
	// that happen after the current set finish
	err := m.autoRefresh.ensureRefreshHoldAtLeast(delay)
	if err != nil {
		return nil, err
	}

	// look for auto refresh changes in progress
	autoRefreshChgsInFlight := []*state.Change{}
	for _, chg := range m.state.Changes() {
		if chg.Kind() == "auto-refresh" && !chg.Status().Ready() {
			autoRefreshChgsInFlight = append(autoRefreshChgsInFlight, chg)
		}
	}

	return autoRefreshChgsInFlight, nil
}

func (m *SnapManager) ensureVulnerableSnapRemoved(name string) error {
	// Do not do anything if we have already done this removal before on this
	// device. This is because if, after we have removed vulnerable snaps the
	// user decides to refresh to a vulnerable version of snapd, that is their
	// choice and furthermore, this removal is itself really just a last minute
	// circumvention for the issue where vulnerable snaps are left in place, we
	// do not intend to ever do this again and instead will unmount or remount
	// vulnerable old snaps as nosuid to prevent the suid snap-confine binaries
	// in them from being available to abuse for fixed vulnerabilies that are
	// not exploitable in the current versions of snapd/core snaps.
	var alreadyRemoved bool
	key := fmt.Sprintf("%s-snap-cve-2022-3328-vuln-removed", name)
	if err := m.state.Get(key, &alreadyRemoved); err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}
	if alreadyRemoved {
		return nil
	}
	var snapSt SnapState
	err := Get(m.state, name, &snapSt)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}
	if errors.Is(err, state.ErrNoState) {
		// not installed, nothing to do
		return nil
	}

	// check if the installed, active version is fixed
	fixedVersionInstalled := false
	inactiveVulnRevisions := []snap.Revision{}
	for _, si := range snapSt.Sequence {
		// check this version
		s := snap.Info{SideInfo: *si}
		ver, _, err := snapdtool.SnapdVersionFromInfoFile(filepath.Join(s.MountDir(), dirs.CoreLibExecDir))
		if err != nil {
			return err
		}
		// res is < 0 if "ver" is lower than "2.57.6"
		res, err := strutil.VersionCompare(ver, "2.57.6")
		if err != nil {
			return err
		}
		revIsVulnerable := (res < 0)
		switch {
		case !revIsVulnerable && si.Revision == snapSt.Current:
			fixedVersionInstalled = true
		case revIsVulnerable && si.Revision == snapSt.Current:
			// The active installed revision is not fixed, we can break out
			// early since we know we won't be able to remove old revisions.
			// Note that we do not attempt to refresh the snap right now, partly
			// because it may not work due to validations on the core/snapd snap
			// on some devices, but also because doing so out of band from
			// normal, controllable refresh schedules introduces non-trivial
			// load on store services and ignores user settings around refresh
			// schedules which we ought to obey as best we can.
			return nil
		case revIsVulnerable && si.Revision != snapSt.Current:
			// si revision is not fixed, but is not active, so it is a candidate
			// for removal
			inactiveVulnRevisions = append(inactiveVulnRevisions, si.Revision)
		default:
			// si revision is not active, but it is fixed, so just ignore it
		}
	}

	if !fixedVersionInstalled {
		return nil
	}

	// remove all the inactive vulnerable revisions
	for _, rev := range inactiveVulnRevisions {
		tss, err := Remove(m.state, name, rev, nil)

		if err != nil {
			// in case of conflict, just trigger another ensure in a little
			// bit and try again later
			if _, ok := err.(*ChangeConflictError); ok {
				m.state.EnsureBefore(time.Minute)
				return nil
			}
			return fmt.Errorf("cannot make task set for removing %s snap: %v", name, err)
		}

		msg := fmt.Sprintf(i18n.G("Remove inactive vulnerable %q snap (%v)"), name, rev)

		chg := m.state.NewChange("remove-snap", msg)
		chg.AddAll(tss)
		chg.Set("snap-names", []string{name})
	}

	// TODO: is it okay to set state here as done or should we do this
	// elsewhere after the change is done somehow?

	// mark state as done
	m.state.Set(key, true)

	// not strictly necessary, but does not hurt to ensure anyways
	m.state.EnsureBefore(0)

	return nil
}

func (m *SnapManager) ensureVulnerableSnapConfineVersionsRemovedOnClassic() error {
	// only remove snaps on classic
	if !release.OnClassic {
		return nil
	}

	m.state.Lock()
	defer m.state.Unlock()

	// we have to remove vulnerable versions of both the core and snapd snaps
	// only when we now have fixed versions installed / active
	// the fixed version is 2.57.6, so if the version of the current core/snapd
	// snap is that or higher, then we proceed (if we didn't already do this)

	if err := m.ensureVulnerableSnapRemoved("snapd"); err != nil {
		return err
	}

	if err := m.ensureVulnerableSnapRemoved("core"); err != nil {
		return err
	}

	return nil
}

// ensureForceDevmodeDropsDevmodeFromState undoes the forced devmode
// in snapstate for forced devmode distros.
func (m *SnapManager) ensureForceDevmodeDropsDevmodeFromState() error {
	if !sandbox.ForceDevMode() {
		return nil
	}

	m.state.Lock()
	defer m.state.Unlock()

	// int because we might want to come back and do a second pass at cleanup
	var fixed int
	if err := m.state.Get("fix-forced-devmode", &fixed); err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}

	if fixed > 0 {
		return nil
	}

	for _, name := range []string{"core", "ubuntu-core"} {
		var snapst SnapState
		if err := Get(m.state, name, &snapst); errors.Is(err, state.ErrNoState) {
			// nothing to see here
			continue
		} else if err != nil {
			// bad
			return err
		}
		if info := snapst.CurrentSideInfo(); info == nil || info.SnapID == "" {
			continue
		}
		snapst.DevMode = false
		Set(m.state, name, &snapst)
	}
	m.state.Set("fix-forced-devmode", 1)

	return nil
}

// changeInFlight returns true if there is any change in the state
// in non-ready state.
func changeInFlight(st *state.State) bool {
	for _, chg := range st.Changes() {
		if !chg.IsReady() {
			// another change already in motion
			return true
		}
	}
	return false
}

// ensureSnapdSnapTransition will migrate systems to use the "snapd" snap
func (m *SnapManager) ensureSnapdSnapTransition() error {
	m.state.Lock()
	defer m.state.Unlock()

	// we only auto-transition people on classic systems, for core we
	// will need to do a proper re-model
	if !release.OnClassic {
		return nil
	}

	// check if snapd snap is installed
	var snapst SnapState
	err := Get(m.state, "snapd", &snapst)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}
	// nothing to do
	if snapst.IsInstalled() {
		return nil
	}

	// check if the user opts into the snapd snap
	optedIntoSnapdTransition, err := optedIntoSnapdSnap(m.state)
	if err != nil {
		return err
	}
	// nothing to do: the user does not want the snapd snap yet
	if !optedIntoSnapdTransition {
		return nil
	}

	// ensure we only transition systems that have snaps already
	installedSnaps, err := NumSnaps(m.state)
	if err != nil {
		return err
	}
	// no installed snaps (yet): do nothing (fresh classic install)
	if installedSnaps == 0 {
		return nil
	}

	// get current core snap and use same channel/user for the snapd snap
	err = Get(m.state, "core", &snapst)
	// Note that state.ErrNoState should never happen in practise. However
	// if it *does* happen we still want to fix those systems by installing
	// the snapd snap.
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}
	coreChannel := snapst.TrackingChannel
	// snapd/core are never blocked on auth so we don't need to copy
	// the userID from the snapst here
	userID := 0

	if changeInFlight(m.state) {
		// check that there is no change in flight already, this is a
		// precaution to ensure the snapd transition is safe
		return nil
	}

	// ensure we limit the retries in case something goes wrong
	var lastSnapdTransitionAttempt time.Time
	err = m.state.Get("snapd-transition-last-retry-time", &lastSnapdTransitionAttempt)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}
	now := time.Now()
	if !lastSnapdTransitionAttempt.IsZero() && lastSnapdTransitionAttempt.Add(snapdTransitionDelayWithRandomess).After(now) {
		return nil
	}
	m.state.Set("snapd-transition-last-retry-time", now)

	var retryCount int
	err = m.state.Get("snapd-transition-retry", &retryCount)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}
	m.state.Set("snapd-transition-retry", retryCount+1)

	ts, err := Install(context.Background(), m.state, "snapd", &RevisionOptions{Channel: coreChannel}, userID, Flags{})
	if err != nil {
		return err
	}

	msg := i18n.G("Transition to the snapd snap")
	chg := m.state.NewChange("transition-to-snapd-snap", msg)
	chg.AddAll(ts)

	return nil
}

// ensureUbuntuCoreTransition will migrate systems that use "ubuntu-core"
// to the new "core" snap
func (m *SnapManager) ensureUbuntuCoreTransition() error {
	m.state.Lock()
	defer m.state.Unlock()

	var snapst SnapState
	err := Get(m.state, "ubuntu-core", &snapst)
	if errors.Is(err, state.ErrNoState) {
		return nil
	}
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}

	// check that there is no change in flight already, this is a
	// precaution to ensure the core transition is safe
	if changeInFlight(m.state) {
		// another change already in motion
		return nil
	}

	// ensure we limit the retries in case something goes wrong
	var lastUbuntuCoreTransitionAttempt time.Time
	err = m.state.Get("ubuntu-core-transition-last-retry-time", &lastUbuntuCoreTransitionAttempt)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}
	now := time.Now()
	if !lastUbuntuCoreTransitionAttempt.IsZero() && lastUbuntuCoreTransitionAttempt.Add(6*time.Hour).After(now) {
		return nil
	}

	tss, trErr := TransitionCore(m.state, "ubuntu-core", "core")
	if _, ok := trErr.(*ChangeConflictError); ok {
		// likely just too early, retry at next Ensure
		return nil
	}

	m.state.Set("ubuntu-core-transition-last-retry-time", now)

	var retryCount int
	err = m.state.Get("ubuntu-core-transition-retry", &retryCount)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}
	m.state.Set("ubuntu-core-transition-retry", retryCount+1)

	if trErr != nil {
		return trErr
	}

	msg := i18n.G("Transition ubuntu-core to core")
	chg := m.state.NewChange("transition-ubuntu-core", msg)
	for _, ts := range tss {
		chg.AddAll(ts)
	}

	return nil
}

// atSeed implements at seeding policy for refreshes.
func (m *SnapManager) atSeed() error {
	m.state.Lock()
	defer m.state.Unlock()
	var seeded bool
	err := m.state.Get("seeded", &seeded)
	if !errors.Is(err, state.ErrNoState) {
		// already seeded or other error
		return err
	}
	if err := m.autoRefresh.AtSeed(); err != nil {
		return err
	}
	if err := m.refreshHints.AtSeed(); err != nil {
		return err
	}
	return nil
}

var (
	localInstallCleanupWait = time.Duration(24 * time.Hour)
	localInstallLastCleanup time.Time
)

// localInstallCleanup removes files that might've been left behind by an
// old aborted local install.
//
// They're usually cleaned up, but if they're created and then snapd
// stops before writing the change to disk (killed, light cut, etc)
// it'll be left behind.
//
// The code that creates the files is in daemon/api.go's postSnaps
func (m *SnapManager) localInstallCleanup() error {
	m.state.Lock()
	defer m.state.Unlock()

	now := time.Now()
	cutoff := now.Add(-localInstallCleanupWait)
	if localInstallLastCleanup.After(cutoff) {
		return nil
	}
	localInstallLastCleanup = now

	d, err := os.Open(dirs.SnapBlobDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer d.Close()

	var filenames []string
	var fis []os.FileInfo
	for err == nil {
		// TODO: if we had fstatat we could avoid a bunch of stats
		fis, err = d.Readdir(100)
		// fis is nil if err isn't
		for _, fi := range fis {
			name := fi.Name()
			if !strings.HasPrefix(name, dirs.LocalInstallBlobTempPrefix) {
				continue
			}
			if fi.ModTime().After(cutoff) {
				continue
			}
			filenames = append(filenames, name)
		}
	}
	if err != io.EOF {
		return err
	}
	return osutil.UnlinkManyAt(d, filenames)
}

func MockEnsuredMountsUpdated(m *SnapManager, ensured bool) (restore func()) {
	osutil.MustBeTestBinary("ensured snap mounts can only be mocked from tests")
	old := m.ensuredMountsUpdated
	m.ensuredMountsUpdated = ensured
	return func() {
		m.ensuredMountsUpdated = old
	}
}

func getSystemD() systemd.Systemd {
	if snapdenv.Preseeding() {
		return systemd.NewEmulationMode(dirs.GlobalRootDir)
	} else {
		return systemd.New(systemd.SystemMode, nil)
	}
}

func (m *SnapManager) ensureMountsUpdated() error {
	m.state.Lock()
	defer m.state.Unlock()

	if m.ensuredMountsUpdated {
		return nil
	}

	// only run after we are seeded
	var seeded bool
	err := m.state.Get("seeded", &seeded)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}
	if !seeded {
		return nil
	}

	allStates, err := All(m.state)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}

	if len(allStates) != 0 {
		sysd := getSystemD()

		for _, snapSt := range allStates {
			info, err := snapSt.CurrentInfo()
			if err != nil {
				return err
			}
			squashfsPath := dirs.StripRootDir(info.MountFile())
			whereDir := dirs.StripRootDir(info.MountDir())
			if _, err = sysd.EnsureMountUnitFile(info.InstanceName(), info.Revision.String(), squashfsPath, whereDir, "squashfs"); err != nil {
				return err
			}
		}
	}

	m.ensuredMountsUpdated = true

	return nil
}

// Ensure implements StateManager.Ensure.
func (m *SnapManager) Ensure() error {
	if m.preseed {
		return nil
	}

	// do not exit right away on error
	errs := []error{
		m.atSeed(),
		m.ensureAliasesV2(),
		m.ensureForceDevmodeDropsDevmodeFromState(),
		m.ensureUbuntuCoreTransition(),
		m.ensureSnapdSnapTransition(),
		// we should check for full regular refreshes before
		// considering issuing a hint only refresh request
		m.autoRefresh.Ensure(),
		m.refreshHints.Ensure(),
		m.catalogRefresh.Ensure(),
		m.localInstallCleanup(),
		m.ensureVulnerableSnapConfineVersionsRemovedOnClassic(),
		m.ensureMountsUpdated(),
	}

	//FIXME: use firstErr helper
	for _, e := range errs {
		if e != nil {
			return e
		}
	}

	return nil
}
