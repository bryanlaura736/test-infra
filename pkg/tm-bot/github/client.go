// Copyright 2019 Copyright (c) 2019 SAP SE or an SAP affiliate company. All rights reserved. This file is licensed under the Apache Software License, v. 2 except as noted otherwise in the LICENSE file.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package github

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/gardener/test-infra/pkg/tm-bot/github/ghval"
	pluginerr "github.com/gardener/test-infra/pkg/tm-bot/plugins/errors"
	"github.com/go-logr/logr"
	"github.com/google/go-github/v27/github"
	"github.com/pkg/errors"
	"net/http"
)

func NewClient(log logr.Logger, ghClient *github.Client, config map[string]json.RawMessage) (Client, error) {
	return &client{
		log:    log,
		config: config,
		client: ghClient,
	}, nil
}

// GetConfig returns the repository configuration for a specific command
func (c *client) GetConfig(name string) (json.RawMessage, error) {
	config, ok := c.config[name]
	if !ok {
		c.log.V(3).Info("no config found", "plugin", name)
		return nil, fmt.Errorf("config not found for command %s", name)
	}
	return config, nil
}

// ResolveConfigValue determines a GitHub config value and returns the referenced
// raw value, file content or commit hash as string
func (c *client) ResolveConfigValue(event *GenericRequestEvent, value *ghval.GitHubValue) (string, error) {
	if value.Value != nil {
		return *value.Value, nil
	}
	if value.PRHead != nil && *value.PRHead {
		pr, _, err := c.client.PullRequests.Get(context.TODO(), event.GetOwnerName(), event.GetRepositoryName(), event.Number)
		if err != nil {
			return "", pluginerr.New("unable to get pr", err.Error())
		}
		return pr.GetHead().GetSHA(), nil
	}
	if value.Path != nil {
		pr, _, err := c.client.PullRequests.Get(context.TODO(), event.GetOwnerName(), event.GetRepositoryName(), event.Number)
		if err != nil {
			return "", pluginerr.New(fmt.Sprintf("unable to get pr for config in path %s", *value.Path), err.Error())
		}
		file, dir, req, err := c.client.Repositories.GetContents(context.TODO(), event.GetOwnerName(), event.GetRepositoryName(), *value.Path, &github.RepositoryContentGetOptions{Ref: pr.GetHead().GetSHA()})
		if err != nil {
			if req != nil && req.StatusCode == http.StatusNotFound {
				return "nil", pluginerr.New(fmt.Sprintf("config path %s cannot be found in %s", *value.Path, pr.GetHead().GetSHA()), err.Error())
			}
			return "", pluginerr.New(fmt.Sprintf("unable to get config in path %s", *value.Path), err.Error())
		}
		if len(dir) != 0 {
			return "", pluginerr.New(fmt.Sprintf("config path %s is a directory not a file", *value.Path), "config path is a directory not a file")
		}

		content, err := file.GetContent()
		if err != nil {
			return "", pluginerr.New(fmt.Sprintf("unable to get config in path %s", *value.Path), err.Error())
		}
		return content, nil
	}
	return "", pluginerr.New("no value is defined", "no value is defined")
}

// UpdateComment edits specific comment and overwrites its message
func (c *client) UpdateComment(event *GenericRequestEvent, commentID int64, message string) error {
	_, _, err := c.client.Issues.EditComment(context.TODO(), event.GetOwnerName(), event.GetRepositoryName(), commentID, &github.IssueComment{
		Body: &message,
	})
	if err != nil {
		return errors.Wrapf(err, "unable to edit comment")
	}

	return nil
}

// Comment responds to an event
func (c *client) Comment(event *GenericRequestEvent, message string) (int64, error) {
	comment, _, err := c.client.Issues.CreateComment(context.TODO(), event.GetOwnerName(), event.GetRepositoryName(), event.Number, &github.IssueComment{
		Body: &message,
	})
	if err != nil {
		return 0, errors.Wrapf(err, "unable to respond to request")
	}

	return comment.GetID(), nil
}

// UpdateStatus updates the status check for a pull request
func (c *client) UpdateStatus(event *GenericRequestEvent, state State, statusContext, description string) error {
	pr, _, err := c.client.PullRequests.Get(context.TODO(), event.GetOwnerName(), event.GetRepositoryName(), event.Number)
	if err != nil {
		return err
	}

	stateString := string(state)
	_, _, err = c.client.Repositories.CreateStatus(context.TODO(), event.GetOwnerName(), event.GetRepositoryName(), pr.GetHead().GetSHA(), &github.RepoStatus{
		State:       &stateString,
		Description: &description,
		Context:     &statusContext,
	})
	return err
}

// IsAuthorized checks if the author of the event is authorized to perform actions on the service
func (c *client) IsAuthorized(event *GenericRequestEvent) bool {
	if UserType(*event.Author.Type) == UserTypeBot {
		return false
	}

	membership, _, err := c.client.Organizations.GetOrgMembership(context.TODO(), event.GetAuthorName(), event.Repository.GetOwner().GetLogin())
	if err != nil {
		c.log.V(3).Info(err.Error())
		return false
	}
	if *membership.State != "active" {
		return false
	}
	return true
}
