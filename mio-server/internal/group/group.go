package group

import "sync"

type Group struct {
	ID      string
	Owner   string
	Members map[string]struct{}
}

type Manager struct {
	mu           sync.Mutex
	clientGroups map[string]string
	groups       map[string]*Group
}

func NewManager() *Manager {
	return &Manager{
		clientGroups: make(map[string]string),
		groups:       make(map[string]*Group),
	}
}

func (m *Manager) RegisterClient(clientID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.clientGroups[clientID]; !ok {
		m.clientGroups[clientID] = ""
	}
}

func (m *Manager) RemoveClient(clientID string) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	groupID := m.clientGroups[clientID]
	if groupID == "" {
		delete(m.clientGroups, clientID)
		return nil
	}
	group, ok := m.groups[groupID]
	if !ok {
		delete(m.clientGroups, clientID)
		return nil
	}
	delete(group.Members, clientID)
	delete(m.clientGroups, clientID)

	if group.Owner == clientID {
		for member := range group.Members {
			group.Owner = member
			break
		}
	}
	if len(group.Members) == 0 {
		delete(m.groups, groupID)
		return nil
	}
	members := make([]string, 0, len(group.Members))
	for member := range group.Members {
		members = append(members, member)
	}
	return members
}

func (m *Manager) AddClient(inviter string, invitee string) (bool, string, []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.clientGroups[invitee]; !ok {
		return false, "Invitee does not exist", nil
	}
	if m.clientGroups[invitee] != "" {
		return false, "Invitee already in group", nil
	}
	groupID := m.clientGroups[inviter]
	if groupID == "" {
		groupID = "group_" + inviter
		m.groups[groupID] = &Group{ID: groupID, Owner: inviter, Members: map[string]struct{}{inviter: {}}}
		m.clientGroups[inviter] = groupID
	}
	group := m.groups[groupID]
	group.Members[invitee] = struct{}{}
	m.clientGroups[invitee] = groupID

	members := make([]string, 0, len(group.Members))
	for member := range group.Members {
		members = append(members, member)
	}
	return true, "Client added to group", members
}

func (m *Manager) RemoveClientFromGroup(remover string, target string) (bool, string, []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	groupID := m.clientGroups[target]
	if groupID == "" {
		return false, "Target not in group", nil
	}
	group := m.groups[groupID]
	if remover != group.Owner && remover != target {
		return false, "Only owner or self can remove", nil
	}
	delete(group.Members, target)
	m.clientGroups[target] = ""
	if len(group.Members) == 0 {
		delete(m.groups, groupID)
		return true, "Group removed", nil
	}
	members := make([]string, 0, len(group.Members))
	for member := range group.Members {
		members = append(members, member)
	}
	return true, "Client removed from group", members
}

func (m *Manager) GetGroupMembers(clientID string) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	groupID := m.clientGroups[clientID]
	if groupID == "" {
		return nil
	}
	group := m.groups[groupID]
	if group == nil {
		return nil
	}
	members := make([]string, 0, len(group.Members))
	for member := range group.Members {
		members = append(members, member)
	}
	return members
}

func (m *Manager) IsOwner(clientID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	groupID := m.clientGroups[clientID]
	if groupID == "" {
		return false
	}
	group := m.groups[groupID]
	if group == nil {
		return false
	}
	return group.Owner == clientID
}
