package control

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"testing"

	"my-movie/internal/database"
	"my-movie/internal/domain"
)

func TestInitializeCreatesRoleGatedAlertChannelsAndOnboarding(t *testing.T) {
	store := newFakeStore()
	channels := &fakeChannels{}
	service := New(store, channels, nil)

	installation, err := service.Initialize(context.Background(), "guild", "owner")
	if err != nil {
		t.Fatal(err)
	}
	if channels.roleName != "영화 알림" || channels.categoryName != "영화 예매 알림" || !channels.restrictedCategory {
		t.Fatalf("role=%q category=%q restricted=%v", channels.roleName, channels.categoryName, channels.restrictedCategory)
	}
	if !reflect.DeepEqual(channels.privateTextNames, []string{"제어"}) {
		t.Fatalf("private channels=%v", channels.privateTextNames)
	}
	wantPublicNames := []string{"공지", "안내"}
	if !reflect.DeepEqual(channels.publicTextNames, wantPublicNames) {
		t.Fatalf("public channels=%v want=%v", channels.publicTextNames, wantPublicNames)
	}
	wantRestrictedNames := []string{"서버-상태", "메가박스-코엑스-돌비", "메가박스-남현아-돌비", "cgv-용산-imax", "cgv-용산-4dx", "cgv-용산-screenx"}
	if !reflect.DeepEqual(channels.restrictedTextNames, wantRestrictedNames) {
		t.Fatalf("restricted channels=%v want=%v", channels.restrictedTextNames, wantRestrictedNames)
	}
	if installation.OwnerUserID != "owner" || installation.ViewerRoleID == "" || installation.NoticeChannelID == "" || installation.GuideChannelID == "" || installation.GuideImageMessageID == "" || installation.GuideMessageID == "" || installation.ControlMessageID == "" || installation.StatusChannelID == "" {
		t.Fatalf("installation=%+v", installation)
	}
	if len(channels.guides) != 1 || channels.guides[0] != installation.GuideChannelID {
		t.Fatalf("guides=%v installation=%+v", channels.guides, installation)
	}
	if len(channels.guideImages) != 1 || channels.guideImages[0] != installation.GuideChannelID {
		t.Fatalf("guide images=%v installation=%+v", channels.guideImages, installation)
	}
	if len(store.installationSaves) < 8 || store.installationSaves[0].ViewerRoleID == "" || store.installationSaves[1].NoticeChannelID == "" || store.installationSaves[2].GuideChannelID == "" || store.installationSaves[3].GuideImageMessageID == "" || store.installationSaves[4].GuideMessageID == "" || store.installationSaves[5].CategoryID == "" || store.installationSaves[6].ControlChannelID == "" {
		t.Fatalf("incremental saves=%+v", store.installationSaves)
	}
	if len(store.states) != 5 {
		t.Fatalf("states=%+v", store.states)
	}
	for _, state := range store.states {
		if state.Enabled {
			t.Fatalf("initial target is enabled: %+v", state)
		}
	}
	if len(channels.panels) != 1 || len(channels.panels[0].Targets) != 5 {
		t.Fatalf("panels=%+v", channels.panels)
	}
}

func TestInitializeMigratesLegacyGuideToImageFirstOrder(t *testing.T) {
	store := newFakeStore()
	store.installation = &database.Installation{
		GuildID: "guild", OwnerUserID: "owner", ViewerRoleID: "viewer", NoticeChannelID: "notice",
		GuideChannelID: "guide-channel", GuideMessageID: "100", CategoryID: "category",
		ControlChannelID: "control", ControlMessageID: "panel", StatusChannelID: "status",
	}
	channels := &fakeChannels{}
	installation, err := New(store, channels, nil).Initialize(context.Background(), "guild", "owner")
	if err != nil {
		t.Fatal(err)
	}
	if installation.GuideImageMessageID == "" || installation.GuideMessageID == "" || installation.GuideMessageID == "100" {
		t.Fatalf("installation=%+v", installation)
	}
	wantOrder := []string{"guide-image", "delete-guide", "guide"}
	var gotOrder []string
	for _, call := range channels.callOrder {
		if call == "guide-image" || call == "delete-guide" || call == "guide" {
			gotOrder = append(gotOrder, call)
		}
	}
	if !reflect.DeepEqual(gotOrder, wantOrder) {
		t.Fatalf("guide order=%v want=%v; all calls=%v", gotOrder, wantOrder, channels.callOrder)
	}
}

func TestInitializeReusesSavedChannelsAndRejectsAnotherOwner(t *testing.T) {
	store := newFakeStore()
	channels := &fakeChannels{}
	service := New(store, channels, nil)
	first, err := service.Initialize(context.Background(), "guild", "owner")
	if err != nil {
		t.Fatal(err)
	}
	channels.resetCalls()
	second, err := service.Initialize(context.Background(), "guild", "owner")
	if err != nil {
		t.Fatal(err)
	}
	if first.CategoryID != second.CategoryID || len(channels.createdIDs) != 0 {
		t.Fatalf("first=%+v second=%+v newly-created=%v", first, second, channels.createdIDs)
	}
	if len(channels.callOrder) < 2 || channels.callOrder[0] != "private:제어" || channels.callOrder[1] != "role:영화 알림" {
		t.Fatalf("unsafe permission update order=%v", channels.callOrder)
	}
	if _, err := service.Initialize(context.Background(), "guild", "intruder"); !errors.Is(err, ErrNotOwner) {
		t.Fatalf("error=%v", err)
	}
}

func TestInitializeDeletesFreshNoticeChannelWhenPersistenceFails(t *testing.T) {
	store := newFakeStore()
	store.failInstallationSaveAt = 2
	channels := &fakeChannels{}
	service := New(store, channels, nil)
	ctx, cancel := context.WithCancel(context.Background())
	store.cancelOnInstallationSaveFailure = cancel

	if _, err := service.Initialize(ctx, "guild", "owner"); err == nil {
		t.Fatal("expected persistence failure")
	}
	if !reflect.DeepEqual(channels.deletedChannels, []string{"channel-공지"}) {
		t.Fatalf("deleted channels=%v", channels.deletedChannels)
	}
	if channels.cleanupContextErr != nil {
		t.Fatalf("cleanup reused canceled request context: %v", channels.cleanupContextErr)
	}
}

func TestInitializeDeletesReplacementCreatedFromStaleNoticeID(t *testing.T) {
	store := newFakeStore()
	store.installation = &database.Installation{GuildID: "guild", OwnerUserID: "owner", ViewerRoleID: "viewer", NoticeChannelID: "stale"}
	store.failInstallationSaveAt = 2
	channels := &fakeChannels{freshPublicNames: map[string]bool{"공지": true}}

	if _, err := New(store, channels, nil).Initialize(context.Background(), "guild", "owner"); err == nil {
		t.Fatal("expected persistence failure")
	}
	if !reflect.DeepEqual(channels.deletedChannels, []string{"replacement-공지"}) {
		t.Fatalf("deleted channels=%v", channels.deletedChannels)
	}
}

func TestJoinAlertsAssignsViewerRole(t *testing.T) {
	store := newFakeStore()
	store.installation = &database.Installation{GuildID: "guild", OwnerUserID: "owner", ViewerRoleID: "viewer"}
	channels := &fakeChannels{}
	service := New(store, channels, nil)

	if err := service.JoinAlerts(context.Background(), "member"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(channels.roleAssignments, [][3]string{{"guild", "member", "viewer"}}) {
		t.Fatalf("assignments=%v", channels.roleAssignments)
	}
}

func TestJoinAlertsRejectsUninitializedRole(t *testing.T) {
	store := newFakeStore()
	store.installation = &database.Installation{GuildID: "guild", OwnerUserID: "owner"}
	if err := New(store, &fakeChannels{}, nil).JoinAlerts(context.Background(), "member"); err == nil {
		t.Fatal("expected initialization error")
	}
}

func TestAnnounceAllowsOnlyOwnerAndFormatsMessage(t *testing.T) {
	store := newFakeStore()
	store.installation = &database.Installation{GuildID: "guild", OwnerUserID: "owner", NoticeChannelID: "notice"}
	channels := &fakeChannels{}
	service := New(store, channels, nil)

	if err := service.Announce(context.Background(), "intruder", "내용"); !errors.Is(err, ErrNotOwner) {
		t.Fatalf("non-owner error=%v", err)
	}
	if err := service.Announce(context.Background(), "owner", "   "); err == nil {
		t.Fatal("expected empty announcement error")
	}
	if err := service.Announce(context.Background(), "owner", "  점검 안내  "); err != nil {
		t.Fatal(err)
	}
	want := [2]string{"notice", "📢 **공지**\n점검 안내"}
	if len(channels.announcements) != 1 || channels.announcements[0] != want {
		t.Fatalf("announcements=%v", channels.announcements)
	}
}

func TestEnableCapturesCurrentTargetAsBaselineBeforeTurningOn(t *testing.T) {
	store := newFakeStore()
	store.installation = &database.Installation{GuildID: "guild", OwnerUserID: "owner"}
	store.states["cgv-yongsan-imax"] = database.TargetState{TargetID: "cgv-yongsan-imax", ChannelID: "imax"}
	provider := &fakeBranchProvider{id: domain.ProviderCGV, showtimes: []domain.Showtime{
		{Provider: domain.ProviderCGV, TargetID: "cgv-yongsan-imax", ExternalID: "existing"},
		{Provider: domain.ProviderCGV, TargetID: "cgv-yongsan-4dx", ExternalID: "other-target"},
	}}
	service := New(store, &fakeChannels{}, map[domain.ProviderID]BranchProvider{domain.ProviderCGV: provider})

	if err := service.Enable(context.Background(), "owner", "cgv-yongsan-imax"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(store.baselines["cgv-yongsan-imax"], []string{"existing"}) {
		t.Fatalf("baseline=%v", store.baselines)
	}
	if !store.states["cgv-yongsan-imax"].Enabled || provider.calls != 1 {
		t.Fatalf("state=%+v provider calls=%d", store.states["cgv-yongsan-imax"], provider.calls)
	}
}

func TestEnableFailureAndNonOwnerLeaveTargetOff(t *testing.T) {
	store := newFakeStore()
	store.installation = &database.Installation{GuildID: "guild", OwnerUserID: "owner"}
	store.states["cgv-yongsan-imax"] = database.TargetState{TargetID: "cgv-yongsan-imax", ChannelID: "imax"}
	provider := &fakeBranchProvider{id: domain.ProviderCGV, err: errors.New("upstream unavailable")}
	service := New(store, &fakeChannels{}, map[domain.ProviderID]BranchProvider{domain.ProviderCGV: provider})

	if err := service.Enable(context.Background(), "intruder", "cgv-yongsan-imax"); !errors.Is(err, ErrNotOwner) {
		t.Fatalf("non-owner error=%v", err)
	}
	if err := service.Enable(context.Background(), "owner", "cgv-yongsan-imax"); err == nil {
		t.Fatal("expected provider error")
	}
	if store.states["cgv-yongsan-imax"].Enabled || len(store.baselines) != 0 {
		t.Fatalf("state=%+v baselines=%v", store.states["cgv-yongsan-imax"], store.baselines)
	}
}

func TestDisableUnavailableTurnsTargetOffAndRefreshesPanel(t *testing.T) {
	store := newFakeStore()
	store.installation = &database.Installation{GuildID: "guild", OwnerUserID: "owner", ControlChannelID: "control", ControlMessageID: "panel"}
	store.states["cgv-yongsan-imax"] = database.TargetState{TargetID: "cgv-yongsan-imax", ChannelID: "imax", Enabled: true}
	channels := &fakeChannels{}
	service := New(store, channels, nil)

	if err := service.DisableUnavailable(context.Background(), "cgv-yongsan-imax"); err != nil {
		t.Fatal(err)
	}
	if store.states["cgv-yongsan-imax"].Enabled || len(channels.panels) != 1 {
		t.Fatalf("state=%+v panels=%+v", store.states["cgv-yongsan-imax"], channels.panels)
	}
}

type fakeStore struct {
	installation                    *database.Installation
	installationSaves               []database.Installation
	failInstallationSaveAt          int
	cancelOnInstallationSaveFailure context.CancelFunc
	states                          map[string]database.TargetState
	baselines                       map[string][]string
}

func newFakeStore() *fakeStore {
	return &fakeStore{states: map[string]database.TargetState{}, baselines: map[string][]string{}}
}
func (s *fakeStore) GetInstallation(context.Context) (database.Installation, error) {
	if s.installation == nil {
		return database.Installation{}, sql.ErrNoRows
	}
	return *s.installation, nil
}
func (s *fakeStore) SaveInstallation(_ context.Context, installation database.Installation) error {
	if len(s.installationSaves)+1 == s.failInstallationSaveAt {
		if s.cancelOnInstallationSaveFailure != nil {
			s.cancelOnInstallationSaveFailure()
		}
		return errors.New("save installation failed")
	}
	copy := installation
	s.installation = &copy
	s.installationSaves = append(s.installationSaves, installation)
	return nil
}
func (s *fakeStore) SaveTargetState(_ context.Context, state database.TargetState) error {
	s.states[state.TargetID] = state
	return nil
}
func (s *fakeStore) ListTargetStates(context.Context) ([]database.TargetState, error) {
	states := make([]database.TargetState, 0, len(s.states))
	for _, state := range s.states {
		states = append(states, state)
	}
	return states, nil
}
func (s *fakeStore) ReplaceBaseline(_ context.Context, targetID string, ids []string) error {
	s.baselines[targetID] = append([]string(nil), ids...)
	return nil
}

type fakeChannels struct {
	roleName            string
	categoryName        string
	categoryOwner       string
	publicCategory      bool
	restrictedCategory  bool
	privateTextNames    []string
	publicTextNames     []string
	restrictedTextNames []string
	createdIDs          []string
	panels              []Panel
	guides              []string
	guideImages         []string
	roleAssignments     [][3]string
	announcements       [][2]string
	deletedRoles        []string
	deletedChannels     []string
	deletedMessages     [][2]string
	freshPublicNames    map[string]bool
	cleanupContextErr   error
	callOrder           []string
}

func (c *fakeChannels) EnsureViewerRole(_ context.Context, _ string, existingID, name string) (string, bool, error) {
	c.callOrder = append(c.callOrder, "role:"+name)
	c.roleName = name
	if existingID != "" {
		return existingID, false, nil
	}
	id := "role-viewer"
	c.createdIDs = append(c.createdIDs, id)
	return id, true, nil
}

func (c *fakeChannels) EnsurePrivateCategory(_ context.Context, _ string, existingID, name, ownerID string) (string, error) {
	c.categoryName, c.categoryOwner = name, ownerID
	if existingID != "" {
		return existingID, nil
	}
	id := "category"
	c.createdIDs = append(c.createdIDs, id)
	return id, nil
}
func (c *fakeChannels) EnsurePrivateTextChannel(_ context.Context, _ string, _ string, existingID, name, _ string) (string, bool, error) {
	c.callOrder = append(c.callOrder, "private:"+name)
	c.privateTextNames = append(c.privateTextNames, name)
	if existingID != "" {
		return existingID, false, nil
	}
	id := "channel-" + name
	c.createdIDs = append(c.createdIDs, id)
	return id, true, nil
}
func (c *fakeChannels) EnsurePublicCategory(_ context.Context, _ string, existingID, name string) (string, error) {
	c.callOrder = append(c.callOrder, "category:public")
	c.categoryName, c.publicCategory = name, true
	if existingID != "" {
		return existingID, nil
	}
	id := "category"
	c.createdIDs = append(c.createdIDs, id)
	return id, nil
}
func (c *fakeChannels) EnsureRestrictedCategory(_ context.Context, _ string, existingID, name, _ string) (string, bool, error) {
	c.callOrder = append(c.callOrder, "category:restricted")
	c.categoryName, c.restrictedCategory = name, true
	if existingID != "" {
		return existingID, false, nil
	}
	id := "category"
	c.createdIDs = append(c.createdIDs, id)
	return id, true, nil
}
func (c *fakeChannels) EnsurePublicTextChannel(_ context.Context, _ string, _ string, existingID, name string) (string, bool, error) {
	c.callOrder = append(c.callOrder, "public:"+name)
	c.publicTextNames = append(c.publicTextNames, name)
	if c.freshPublicNames[name] {
		return "replacement-" + name, true, nil
	}
	if existingID != "" {
		return existingID, false, nil
	}
	id := "channel-" + name
	c.createdIDs = append(c.createdIDs, id)
	return id, true, nil
}
func (c *fakeChannels) EnsureRestrictedTextChannel(_ context.Context, _ string, _ string, existingID, name, _ string) (string, bool, error) {
	c.callOrder = append(c.callOrder, "restricted:"+name)
	c.restrictedTextNames = append(c.restrictedTextNames, name)
	if existingID != "" {
		return existingID, false, nil
	}
	id := "channel-" + name
	c.createdIDs = append(c.createdIDs, id)
	return id, true, nil
}
func (c *fakeChannels) UpsertPanel(_ context.Context, _ string, existingID string, panel Panel) (string, bool, error) {
	c.panels = append(c.panels, panel)
	if existingID != "" {
		return existingID, false, nil
	}
	return "panel", true, nil
}
func (c *fakeChannels) UpsertGuide(_ context.Context, channelID, existingID, imageMessageID string) (string, bool, error) {
	if existingID == "100" && imageMessageID == "200" {
		c.callOrder = append(c.callOrder, "delete-guide")
		existingID = ""
	}
	c.callOrder = append(c.callOrder, "guide")
	c.guides = append(c.guides, channelID)
	if existingID != "" {
		return existingID, false, nil
	}
	return "guide-message", true, nil
}
func (c *fakeChannels) UpsertGuideImage(_ context.Context, channelID, existingID string) (string, bool, error) {
	c.callOrder = append(c.callOrder, "guide-image")
	c.guideImages = append(c.guideImages, channelID)
	if existingID != "" {
		return existingID, false, nil
	}
	return "200", true, nil
}
func (c *fakeChannels) AddMemberRole(_ context.Context, guildID, userID, roleID string) error {
	c.roleAssignments = append(c.roleAssignments, [3]string{guildID, userID, roleID})
	return nil
}
func (c *fakeChannels) SendAnnouncement(_ context.Context, channelID, content string) error {
	c.announcements = append(c.announcements, [2]string{channelID, content})
	return nil
}
func (c *fakeChannels) DeleteRole(_ context.Context, _, roleID string) error {
	c.deletedRoles = append(c.deletedRoles, roleID)
	return nil
}
func (c *fakeChannels) DeleteChannel(ctx context.Context, channelID string) error {
	c.cleanupContextErr = ctx.Err()
	c.deletedChannels = append(c.deletedChannels, channelID)
	return nil
}
func (c *fakeChannels) DeleteMessage(_ context.Context, channelID, messageID string) error {
	c.deletedMessages = append(c.deletedMessages, [2]string{channelID, messageID})
	return nil
}
func (c *fakeChannels) resetCalls() {
	c.privateTextNames = nil
	c.publicTextNames = nil
	c.restrictedTextNames = nil
	c.createdIDs = nil
	c.panels = nil
	c.callOrder = nil
}

type fakeBranchProvider struct {
	id        domain.ProviderID
	showtimes []domain.Showtime
	err       error
	calls     int
}

func (p *fakeBranchProvider) ID() domain.ProviderID { return p.id }
func (p *fakeBranchProvider) FetchBranchSnapshot(context.Context, domain.Branch) ([]domain.Showtime, error) {
	p.calls++
	return p.showtimes, p.err
}
