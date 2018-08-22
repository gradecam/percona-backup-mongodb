// Code generated by MockGen. DO NOT EDIT.
// Source: github.com/percona/mongodb-backup/proto/messages (interfaces: MessagesServer)

// Package mock_messages is a generated GoMock package.
package mock_messages

import (
	gomock "github.com/golang/mock/gomock"
	messages "github.com/percona/mongodb-backup/proto/messages"
	reflect "reflect"
)

// MockMessagesServer is a mock of MessagesServer interface
type MockMessagesServer struct {
	ctrl     *gomock.Controller
	recorder *MockMessagesServerMockRecorder
}

// MockMessagesServerMockRecorder is the mock recorder for MockMessagesServer
type MockMessagesServerMockRecorder struct {
	mock *MockMessagesServer
}

// NewMockMessagesServer creates a new mock instance
func NewMockMessagesServer(ctrl *gomock.Controller) *MockMessagesServer {
	mock := &MockMessagesServer{ctrl: ctrl}
	mock.recorder = &MockMessagesServerMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use
func (m *MockMessagesServer) EXPECT() *MockMessagesServerMockRecorder {
	return m.recorder
}

// MessagesChat mocks base method
func (m *MockMessagesServer) MessagesChat(arg0 messages.Messages_MessagesChatServer) error {
	ret := m.ctrl.Call(m, "MessagesChat", arg0)
	ret0, _ := ret[0].(error)
	return ret0
}

// MessagesChat indicates an expected call of MessagesChat
func (mr *MockMessagesServerMockRecorder) MessagesChat(arg0 interface{}) *gomock.Call {
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "MessagesChat", reflect.TypeOf((*MockMessagesServer)(nil).MessagesChat), arg0)
}