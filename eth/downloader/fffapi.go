// Copyright 2015 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package downloader

import (
	"context"
	"sync"
	"github.com/fff-chain/go-fff/common/gopool"
	"github.com/fff-chain/go-fff/event"
	"github.com/fff-chain/go-fff/rpc"
)

// PublicDownloaderAPI provides an API which gives information about the current synchronisation status.
// It offers only methods that operates on data that can be available to anyone without security risks.
type FFFPublicDownloaderAPI struct {
	d                         *Downloader
	mux                       *event.TypeMux
	installSyncSubscription   chan chan interface{}
	uninstallSyncSubscription chan *uninstallSyncSubscriptionRequest
}

// NewPublicDownloaderAPI create a new PublicDownloaderAPI. The API has an internal event loop that
// listens for events from the downloader through the global event mux. In case it receives one of
// these events it broadcasts it to all syncing subscriptions that are installed through the
// installSyncSubscription channel.
func NewFFFPublicDownloaderAPI(d *Downloader, m *event.TypeMux) *FFFPublicDownloaderAPI {
	api := &FFFPublicDownloaderAPI{
		d:                         d,
		mux:                       m,
		installSyncSubscription:   make(chan chan interface{}),
		uninstallSyncSubscription: make(chan *uninstallSyncSubscriptionRequest),
	}

	go api.eventLoop()

	return api
}

// eventLoop runs a loop until the event mux closes. It will install and uninstall new
// sync subscriptions and broadcasts sync status updates to the installed sync subscriptions.
func (api *FFFPublicDownloaderAPI) eventLoop() {
	var (
		sub               = api.mux.Subscribe(StartEvent{}, DoneEvent{}, FailedEvent{})
		syncSubscriptions = make(map[chan interface{}]struct{})
	)

	for {
		select {
		case i := <-api.installSyncSubscription:
			syncSubscriptions[i] = struct{}{}
		case u := <-api.uninstallSyncSubscription:
			delete(syncSubscriptions, u.c)
			close(u.uninstalled)
		case event := <-sub.Chan():
			if event == nil {
				return
			}

			var notification interface{}
			switch event.Data.(type) {
			case StartEvent:
				notification = &SyncingResult{
					Syncing: true,
					Status:  api.d.Progress(),
				}
			case DoneEvent, FailedEvent:
				notification = false
			}
			// broadcast
			for c := range syncSubscriptions {
				c <- notification
			}
		}
	}
}

// Syncing provides information when this nodes starts synchronising with the Ethereum network and when it's finished.
func (api *FFFPublicDownloaderAPI) Syncing(ctx context.Context) (*rpc.Subscription, error) {
	notifier, supported := rpc.NotifierFromContext(ctx)
	if !supported {
		return &rpc.Subscription{}, rpc.ErrNotificationsUnsupported
	}

	rpcSub := notifier.CreateSubscription()

	gopool.Submit(func() {
		statuses := make(chan interface{})
		sub := api.SubscribeSyncStatus(statuses)

		for {
			select {
			case status := <-statuses:
				notifier.Notify(rpcSub.ID, status)
			case <-rpcSub.Err():
				sub.Unsubscribe()
				return
			case <-notifier.Closed():
				sub.Unsubscribe()
				return
			}
		}
	})

	return rpcSub, nil
}

// SyncStatusSubscription represents a syncing subscription.
type FFFSyncStatusSubscription struct {
	api       *FFFPublicDownloaderAPI // register subscription in event loop of this api instance
	c         chan interface{}     // channel where events are broadcasted to
	unsubOnce sync.Once            // make sure unsubscribe logic is executed once
}
// SubscribeSyncStatus creates a subscription that will broadcast new synchronisation updates.
// The given channel must receive interface values, the result can either
func (api *FFFPublicDownloaderAPI) SubscribeSyncStatus(status chan interface{}) *FFFSyncStatusSubscription {
	api.installSyncSubscription <- status
	return &FFFSyncStatusSubscription{api: api, c: status}
}
