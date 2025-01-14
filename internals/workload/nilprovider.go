// Copyright (c) 2025 Canonical Ltd
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package workload

var _ Provider = (*NilProvider)(nil)

type NilProvider struct{}

func (NilProvider) Environment(name string) map[string]string {
	return nil
}

func (NilProvider) UserInfo(name string) (uid *int, gid *int) {
	return nil, nil
}

func (NilProvider) Supported() bool {
	return false
}

func (NilProvider) Exists(name string) bool {
	return false
}
