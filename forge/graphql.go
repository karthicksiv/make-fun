package main

// GraphQL v4 at /api/graphql: the schema (in SDL), its resolvers over the store, and the HTTP
// endpoint with single and batched operations.

import (
	"encoding/base64"
	"encoding/json"
	"strconv"
	"strings"
)

const forgeSDL = `
schema {
  query: Query
  mutation: Mutation
}

"An ISO-8601 encoded UTC date string."
scalar DateTime
"An RFC 3986, RFC 3987, and RFC 6570 (level 4) compliant URI string."
scalar URI
"A Git object ID."
scalar GitObjectID
"A string containing HTML code."
scalar HTML

"An object with an ID."
interface Node {
  "ID of the object."
  id: ID!
}

"Represents an object which can take actions on GitHub."
interface Actor {
  login: String!
  url: URI!
  avatarUrl: URI!
}

"Represents an owner of a Repository."
interface RepositoryOwner {
  id: ID!
  login: String!
  url: URI!
  avatarUrl: URI!
}

"Represents a Git object."
interface GitObject {
  id: ID!
  oid: GitObjectID!
}

"The query root of the GraphQL interface."
type Query {
  "The currently authenticated user."
  viewer: User!
  "Lookup a given repository by the owner and repository name."
  repository(owner: String!, name: String!, followRenames: Boolean = true): Repository
  "Fetches an object given its ID."
  node(id: ID!): Node
  "Lookup nodes by a list of IDs."
  nodes(ids: [ID!]!): [Node]!
  "Lookup a user by login."
  user(login: String!): User
  "Lookup a repository owner (ie. either a User or an Organization) by login."
  repositoryOwner(login: String!): RepositoryOwner
  "Perform a search across resources, returning a maximum of 1,000 results."
  search(query: String!, type: SearchType!, first: Int, after: String, last: Int, before: String): SearchResultItemConnection!
  "The client's rate limit information."
  rateLimit: RateLimit
}

"The root query for implementing GraphQL mutations."
type Mutation {
  "Creates a new issue."
  createIssue(input: CreateIssueInput!): CreateIssuePayload
  "Updates an Issue."
  updateIssue(input: UpdateIssueInput!): UpdateIssuePayload
  "Close an issue."
  closeIssue(input: CloseIssueInput!): CloseIssuePayload
  "Reopen a issue."
  reopenIssue(input: ReopenIssueInput!): ReopenIssuePayload
  "Adds a comment to an Issue or Pull Request."
  addComment(input: AddCommentInput!): AddCommentPayload
  "Create a new pull request."
  createPullRequest(input: CreatePullRequestInput!): CreatePullRequestPayload
  "Update a pull request."
  updatePullRequest(input: UpdatePullRequestInput!): UpdatePullRequestPayload
  "Merge a pull request."
  mergePullRequest(input: MergePullRequestInput!): MergePullRequestPayload
  "Close a pull request."
  closePullRequest(input: ClosePullRequestInput!): ClosePullRequestPayload
  "Reopen a pull request."
  reopenPullRequest(input: ReopenPullRequestInput!): ReopenPullRequestPayload
  "Create a new repository."
  createRepository(input: CreateRepositoryInput!): CreateRepositoryPayload
}

"Represents the client's rate limit."
type RateLimit {
  limit: Int!
  cost: Int!
  remaining: Int!
  used: Int!
  nodeCount: Int!
  resetAt: DateTime!
}

"A user is an individual's account on GitHub that owns repositories and can make new content."
type User implements Node & Actor & RepositoryOwner {
  id: ID!
  databaseId: Int
  login: String!
  name: String
  email: String!
  url: URI!
  avatarUrl: URI!
  isViewer: Boolean!
  createdAt: DateTime!
  "A list of repositories that the user owns."
  repositories(first: Int, after: String, last: Int, before: String): RepositoryConnection!
  "Find Repository."
  repository(name: String!): Repository
}

"A repository contains the content for a project."
type Repository implements Node {
  id: ID!
  "Identifies the primary key from the database."
  databaseId: Int
  "The name of the repository."
  name: String!
  "The repository's name with owner."
  nameWithOwner: String!
  "The description of the repository."
  description: String
  "The HTTP URL for this repository"
  url: URI!
  "The SSH URL to clone this repository"
  sshUrl: String!
  "The repository's URL."
  homepageUrl: URI
  "Identifies if the repository is private or internal."
  isPrivate: Boolean!
  "Identifies if the repository is a fork."
  isFork: Boolean!
  "Indicates if the repository is unmaintained."
  isArchived: Boolean!
  "Returns whether or not this repository is empty."
  isEmpty: Boolean!
  "Indicates the repository's visibility level."
  visibility: RepositoryVisibility!
  "The User owner of the repository."
  owner: RepositoryOwner!
  "The Ref associated with the repository's default branch."
  defaultBranchRef: Ref
  "The users permission level on the repository."
  viewerPermission: RepositoryPermission
  "Indicates whether the viewer has admin permissions on this repository."
  viewerCanAdminister: Boolean!
  "Indicates if the repository has issues feature enabled."
  hasIssuesEnabled: Boolean!
  "Indicates if the repository has wiki feature enabled."
  hasWikiEnabled: Boolean!
  "Indicates if the repository has the Projects feature enabled."
  hasProjectsEnabled: Boolean!
  "Whether or not PRs are merged with a merge commit on this repository."
  mergeCommitAllowed: Boolean!
  "Whether or not squash-merging is enabled on this repository."
  squashMergeAllowed: Boolean!
  "Whether or not rebase-merging is enabled on this repository."
  rebaseMergeAllowed: Boolean!
  "Identifies the date and time when the object was created."
  createdAt: DateTime!
  "Identifies the date and time when the object was last updated."
  updatedAt: DateTime!
  "Identifies the date and time when the repository was last pushed to."
  pushedAt: DateTime
  "Returns a count of how many stargazers there are on this object"
  stargazerCount: Int!
  "Returns how many forks there are of this repository in the whole network."
  forkCount: Int!
  "Returns a single issue from the current repository by number."
  issue(number: Int!): Issue
  "A list of issues that have been opened in the repository."
  issues(first: Int, after: String, last: Int, before: String, states: [IssueState!], labels: [String!], orderBy: IssueOrder, filterBy: IssueFilters): IssueConnection!
  "Returns a single pull request from the current repository by number."
  pullRequest(number: Int!): PullRequest
  "A list of pull requests that have been opened in the repository."
  pullRequests(first: Int, after: String, last: Int, before: String, states: [PullRequestState!], labels: [String!], headRefName: String, baseRefName: String, orderBy: IssueOrder): PullRequestConnection!
  "Returns a single issue-like object from the current repository by number."
  issueOrPullRequest(number: Int!): IssueOrPullRequest
  "Returns a single label by name"
  label(name: String!): Label
  "A list of labels associated with the repository."
  labels(first: Int, after: String, last: Int, before: String, query: String): LabelConnection
  "Fetch a given ref from the repository"
  ref(qualifiedName: String!): Ref
  "Fetch a list of refs from the repository"
  refs(refPrefix: String!, first: Int, after: String, last: Int, before: String): RefConnection
}

"Represents a Git reference."
type Ref implements Node {
  id: ID!
  "The ref name."
  name: String!
  "The ref's prefix, such as refs/heads/ or refs/tags/."
  prefix: String!
  "The object the ref points to."
  target: GitObject
}

"Represents a Git commit."
type Commit implements Node & GitObject {
  id: ID!
  oid: GitObjectID!
  "An abbreviated version of the Git object ID"
  abbreviatedOid: String!
  "The Git commit message"
  message: String!
  "The Git commit message headline"
  messageHeadline: String!
  "The Git commit message body"
  messageBody: String!
  "The datetime when this commit was committed."
  committedDate: DateTime!
  "The datetime when this commit was authored."
  authoredDate: DateTime!
  "The HTTP URL for this commit"
  url: URI!
}

"A label for categorizing Issues, Pull Requests, Milestones, or Discussions with a given Repository."
type Label implements Node {
  id: ID!
  name: String!
  color: String!
  description: String
  isDefault: Boolean!
  url: URI!
}

"An Issue is a place to discuss ideas, enhancements, tasks, and bugs for a project."
type Issue implements Node {
  id: ID!
  "Identifies the primary key from the database."
  databaseId: Int
  "Identifies the issue number."
  number: Int!
  "Identifies the issue title."
  title: String!
  "Identifies the body of the issue."
  body: String!
  "Identifies the body of the issue rendered to text."
  bodyText: String!
  "Identifies the state of the issue."
  state: IssueState!
  "Identifies the reason for the issue state."
  stateReason: IssueStateReason
  "Indicates if the object is closed (definition of closed may depend on type)"
  closed: Boolean!
  "Identifies the date and time when the object was closed."
  closedAt: DateTime
  "Identifies the date and time when the object was created."
  createdAt: DateTime!
  "Identifies the date and time when the object was last updated."
  updatedAt: DateTime!
  "The HTTP URL for this issue"
  url: URI!
  "true if the object is locked"
  locked: Boolean!
  "The actor who authored the comment."
  author: Actor
  "The repository associated with this node."
  repository: Repository!
  "A list of labels associated with the object."
  labels(first: Int, after: String, last: Int, before: String): LabelConnection
  "A list of comments associated with the Issue."
  comments(first: Int, after: String, last: Int, before: String): IssueCommentConnection!
  "A list of Users assigned to this object."
  assignees(first: Int, after: String, last: Int, before: String): UserConnection!
}

"A repository pull request."
type PullRequest implements Node {
  id: ID!
  "Identifies the primary key from the database."
  databaseId: Int
  "Identifies the pull request number."
  number: Int!
  "Identifies the pull request title."
  title: String!
  "The body as Markdown."
  body: String!
  "The body rendered to text."
  bodyText: String!
  "Identifies the state of the pull request."
  state: PullRequestState!
  "Whether or not the pull request was merged."
  merged: Boolean!
  "The date and time that the pull request was merged."
  mergedAt: DateTime
  "true if the pull request is closed"
  closed: Boolean!
  "Identifies the date and time when the object was closed."
  closedAt: DateTime
  "Identifies if the pull request is a draft."
  isDraft: Boolean!
  "Identifies the name of the head Ref associated with the pull request, even if the ref has been deleted."
  headRefName: String!
  "Identifies the name of the base Ref associated with the pull request, even if the ref has been deleted."
  baseRefName: String!
  "Identifies the oid of the head ref associated with the pull request, even if the ref has been deleted."
  headRefOid: GitObjectID!
  "Identifies the oid of the base ref associated with the pull request, even if the ref has been deleted."
  baseRefOid: GitObjectID!
  "Whether or not the pull request can be merged based on the existence of merge conflicts."
  mergeable: MergeableState!
  "Detailed information about the current pull request merge state status."
  mergeStateStatus: MergeStateStatus!
  "The commit that was created when this pull request was merged."
  mergeCommit: Commit
  "The head and base repositories are different."
  isCrossRepository: Boolean!
  "Indicates whether maintainers can modify the pull request."
  maintainerCanModify: Boolean!
  "The number of additions in this pull request."
  additions: Int!
  "The number of deletions in this pull request."
  deletions: Int!
  "The number of changed files in this pull request."
  changedFiles: Int!
  "Identifies the date and time when the object was created."
  createdAt: DateTime!
  "Identifies the date and time when the object was last updated."
  updatedAt: DateTime!
  "The HTTP URL for this pull request."
  url: URI!
  "true if the pull request is locked"
  locked: Boolean!
  "The actor who authored the comment."
  author: Actor
  "The repository associated with this node."
  repository: Repository!
  "The repository associated with this pull request's head Ref."
  headRepository: Repository
  "The repository associated with this pull request's base Ref."
  baseRepository: Repository
  "The owner of the repository associated with this pull request's head Ref."
  headRepositoryOwner: RepositoryOwner
  "Lists the files changed within this pull request."
  files(first: Int, after: String, last: Int, before: String): PullRequestChangedFileConnection
  "A list of labels associated with the object."
  labels(first: Int, after: String, last: Int, before: String): LabelConnection
  "A list of comments associated with the pull request."
  comments(first: Int, after: String, last: Int, before: String): IssueCommentConnection!
  "A list of Users assigned to this object."
  assignees(first: Int, after: String, last: Int, before: String): UserConnection!
}

"Represents a comment on an Issue."
type IssueComment implements Node {
  id: ID!
  databaseId: Int
  "The body as Markdown."
  body: String!
  "The body rendered to text."
  bodyText: String!
  "The actor who authored the comment."
  author: Actor
  createdAt: DateTime!
  updatedAt: DateTime!
  "The HTTP URL for this issue comment"
  url: URI!
  "Identifies the issue associated with the comment."
  issue: Issue
}

"A file changed in a pull request."
type PullRequestChangedFile {
  "The path of the file."
  path: String!
  "The number of additions to the file."
  additions: Int!
  "The number of deletions to the file."
  deletions: Int!
  "How the file was changed in this PullRequest"
  changeType: PatchStatus!
}

"Information about pagination in a connection."
type PageInfo {
  "When paginating forwards, are there more items?"
  hasNextPage: Boolean!
  "When paginating backwards, are there more items?"
  hasPreviousPage: Boolean!
  "When paginating backwards, the cursor to continue."
  startCursor: String
  "When paginating forwards, the cursor to continue."
  endCursor: String
}

type IssueConnection {
  edges: [IssueEdge]
  nodes: [Issue]
  pageInfo: PageInfo!
  totalCount: Int!
}

type IssueEdge {
  cursor: String!
  node: Issue
}

type PullRequestConnection {
  edges: [PullRequestEdge]
  nodes: [PullRequest]
  pageInfo: PageInfo!
  totalCount: Int!
}

type PullRequestEdge {
  cursor: String!
  node: PullRequest
}

type PullRequestChangedFileConnection {
  edges: [PullRequestChangedFileEdge]
  nodes: [PullRequestChangedFile]
  pageInfo: PageInfo!
  totalCount: Int!
}

type PullRequestChangedFileEdge {
  cursor: String!
  node: PullRequestChangedFile
}

type IssueCommentConnection {
  edges: [IssueCommentEdge]
  nodes: [IssueComment]
  pageInfo: PageInfo!
  totalCount: Int!
}

type IssueCommentEdge {
  cursor: String!
  node: IssueComment
}

type LabelConnection {
  edges: [LabelEdge]
  nodes: [Label]
  pageInfo: PageInfo!
  totalCount: Int!
}

type LabelEdge {
  cursor: String!
  node: Label
}

type UserConnection {
  edges: [UserEdge]
  nodes: [User]
  pageInfo: PageInfo!
  totalCount: Int!
}

type UserEdge {
  cursor: String!
  node: User
}

type RepositoryConnection {
  edges: [RepositoryEdge]
  nodes: [Repository]
  pageInfo: PageInfo!
  totalCount: Int!
}

type RepositoryEdge {
  cursor: String!
  node: Repository
}

type RefConnection {
  edges: [RefEdge]
  nodes: [Ref]
  pageInfo: PageInfo!
  totalCount: Int!
}

type RefEdge {
  cursor: String!
  node: Ref
}

"The results of a search."
union SearchResultItem = Issue | PullRequest | Repository | User

"Used for return value of Repository.issueOrPullRequest."
union IssueOrPullRequest = Issue | PullRequest

type SearchResultItemConnection {
  edges: [SearchResultItemEdge]
  nodes: [SearchResultItem]
  pageInfo: PageInfo!
  issueCount: Int!
  repositoryCount: Int!
  userCount: Int!
}

type SearchResultItemEdge {
  cursor: String!
  node: SearchResultItem
}

type CreateIssuePayload {
  clientMutationId: String
  issue: Issue
}

type UpdateIssuePayload {
  clientMutationId: String
  issue: Issue
}

type CloseIssuePayload {
  clientMutationId: String
  issue: Issue
}

type ReopenIssuePayload {
  clientMutationId: String
  issue: Issue
}

type AddCommentPayload {
  clientMutationId: String
  commentEdge: IssueCommentEdge
  subject: Node
}

type CreatePullRequestPayload {
  clientMutationId: String
  pullRequest: PullRequest
}

type UpdatePullRequestPayload {
  clientMutationId: String
  pullRequest: PullRequest
}

type MergePullRequestPayload {
  clientMutationId: String
  pullRequest: PullRequest
}

type ClosePullRequestPayload {
  clientMutationId: String
  pullRequest: PullRequest
}

type ReopenPullRequestPayload {
  clientMutationId: String
  pullRequest: PullRequest
}

type CreateRepositoryPayload {
  clientMutationId: String
  repository: Repository
}

input CreateIssueInput {
  repositoryId: ID!
  title: String!
  body: String
  labelIds: [ID!]
  assigneeIds: [ID!]
  clientMutationId: String
}

input UpdateIssueInput {
  id: ID!
  title: String
  body: String
  state: IssueState
  labelIds: [ID!]
  clientMutationId: String
}

input CloseIssueInput {
  issueId: ID!
  stateReason: IssueClosedStateReason
  clientMutationId: String
}

input ReopenIssueInput {
  issueId: ID!
  clientMutationId: String
}

input AddCommentInput {
  subjectId: ID!
  body: String!
  clientMutationId: String
}

input CreatePullRequestInput {
  repositoryId: ID!
  baseRefName: String!
  headRefName: String!
  title: String!
  body: String
  draft: Boolean = false
  maintainerCanModify: Boolean = true
  headRepositoryId: ID
  clientMutationId: String
}

input UpdatePullRequestInput {
  pullRequestId: ID!
  title: String
  body: String
  baseRefName: String
  state: PullRequestUpdateState
  clientMutationId: String
}

input MergePullRequestInput {
  pullRequestId: ID!
  mergeMethod: PullRequestMergeMethod = MERGE
  commitHeadline: String
  commitBody: String
  expectedHeadOid: GitObjectID
  authorEmail: String
  clientMutationId: String
}

input ClosePullRequestInput {
  pullRequestId: ID!
  clientMutationId: String
}

input ReopenPullRequestInput {
  pullRequestId: ID!
  clientMutationId: String
}

input CreateRepositoryInput {
  name: String!
  visibility: RepositoryVisibility!
  description: String
  ownerId: ID
  homepageUrl: URI
  hasIssuesEnabled: Boolean = true
  hasWikiEnabled: Boolean = false
  clientMutationId: String
}

"Ways in which lists of issues can be ordered upon return."
input IssueOrder {
  field: IssueOrderField!
  direction: OrderDirection!
}

"Ways in which to filter lists of issues."
input IssueFilters {
  states: [IssueState!]
  labels: [String!]
  createdBy: String
  assignee: String
}

"The possible states of an issue."
enum IssueState {
  "An issue that is still open"
  OPEN
  "An issue that has been closed"
  CLOSED
}

"The possible state reasons of an issue."
enum IssueStateReason {
  REOPENED
  NOT_PLANNED
  COMPLETED
  DUPLICATE
}

"The possible state reasons of a closed issue."
enum IssueClosedStateReason {
  COMPLETED
  NOT_PLANNED
  DUPLICATE
}

"The possible states of a pull request."
enum PullRequestState {
  "A pull request that is still open."
  OPEN
  "A pull request that has been closed without being merged."
  CLOSED
  "A pull request that has been closed by being merged."
  MERGED
}

"The possible target states when updating a pull request."
enum PullRequestUpdateState {
  OPEN
  CLOSED
}

"Whether or not a PullRequest can be merged."
enum MergeableState {
  "The pull request can be merged."
  MERGEABLE
  "The pull request cannot be merged due to merge conflicts."
  CONFLICTING
  "The mergeability of the pull request is still being calculated."
  UNKNOWN
}

"Detailed status information about a pull request merge."
enum MergeStateStatus {
  DIRTY
  UNKNOWN
  BLOCKED
  BEHIND
  DRAFT
  UNSTABLE
  HAS_HOOKS
  CLEAN
}

"Represents available types of methods to use when merging a pull request."
enum PullRequestMergeMethod {
  "Add all commits from the head branch to the base branch with a merge commit."
  MERGE
  "Combine all commits from the head branch into a single commit in the base branch."
  SQUASH
  "Add all commits from the head branch onto the base branch individually."
  REBASE
}

"The access level to a repository"
enum RepositoryPermission {
  ADMIN
  MAINTAIN
  WRITE
  TRIAGE
  READ
}

"The repository's visibility level."
enum RepositoryVisibility {
  PRIVATE
  PUBLIC
  INTERNAL
}

"Properties by which issue connections can be ordered."
enum IssueOrderField {
  CREATED_AT
  UPDATED_AT
  COMMENTS
}

"Possible directions in which to order a list of items when provided an orderBy argument."
enum OrderDirection {
  ASC
  DESC
}

"Represents the individual results of a search."
enum SearchType {
  "Returns results matching issues in repositories."
  ISSUE
  "Returns results matching issues in repositories."
  ISSUE_ADVANCED
  "Returns results matching repositories."
  REPOSITORY
  "Returns results matching users and organizations on GitHub."
  USER
  "Returns matching discussions in repositories."
  DISCUSSION
}

"The possible types of patch statuses."
enum PatchStatus {
  ADDED
  DELETED
  RENAMED
  COPIED
  MODIFIED
  CHANGED
}
`

// ---------------------------------------------------------------- resolved objects

type gUser struct {
	c *gqlExec
	u *User
}

func (g gUser) gqlTypeName() string { return "User" }

func (g gUser) gqlField(name string) (any, bool) {
	u, base := g.u, g.c.base
	switch name {
	case "id":
		return userNodeID(u), true
	case "databaseId":
		return u.ID, true
	case "login":
		return u.Login, true
	case "name":
		if u.Name == "" {
			return nil, true
		}
		return u.Name, true
	case "email":
		return u.Email, true
	case "url":
		return base + "/" + u.Login, true
	case "avatarUrl":
		return base + "/avatars/u/" + strconv.FormatInt(u.ID, 10), true
	case "isViewer":
		return u == g.c.app.viewer, true
	case "createdAt":
		return formatTime(g.c.app.base), true
	}
	return nil, false
}

type gRepo struct {
	c *gqlExec
	r *Repo
}

func (g gRepo) gqlTypeName() string { return "Repository" }

func (g gRepo) gqlField(name string) (any, bool) {
	r, base := g.r, g.c.base
	switch name {
	case "id":
		return nodeID("Repository", r.ID), true
	case "databaseId":
		return r.ID, true
	case "name":
		return r.Name, true
	case "nameWithOwner":
		return r.FullName(), true
	case "description":
		return nullableString(r.Description, r.HasDesc), true
	case "url":
		return base + "/" + r.FullName(), true
	case "sshUrl":
		return "git@" + strings.TrimPrefix(base, "http://") + ":" + r.FullName() + ".git", true
	case "homepageUrl":
		if r.Homepage == "" {
			return nil, true
		}
		return r.Homepage, true
	case "isPrivate":
		return r.Private, true
	case "isFork":
		return false, true
	case "isArchived":
		return r.Archived, true
	case "isEmpty":
		return len(r.sortedRefs()) == 0, true
	case "visibility":
		if r.Private {
			return "PRIVATE", true
		}
		return "PUBLIC", true
	case "owner":
		return gUser{g.c, r.Owner}, true
	case "defaultBranchRef":
		tip := r.Branch(r.DefaultBranch)
		if tip.IsZero() {
			return nil, true
		}
		return gRef{g.c, r, r.headRef(), tip}, true
	case "viewerPermission":
		return "ADMIN", true
	case "viewerCanAdminister", "mergeCommitAllowed", "squashMergeAllowed", "rebaseMergeAllowed":
		return true, true
	case "hasIssuesEnabled":
		return r.HasIssues, true
	case "hasWikiEnabled":
		return r.HasWiki, true
	case "hasProjectsEnabled":
		return r.HasProjects, true
	case "createdAt":
		return formatTime(r.CreatedAt), true
	case "updatedAt":
		return formatTime(r.UpdatedAt), true
	case "pushedAt":
		return formatTime(r.PushedAt), true
	case "stargazerCount", "forkCount":
		return 0, true
	}
	return nil, false
}

type gRef struct {
	c    *gqlExec
	r    *Repo
	name string // full name, refs/heads/main
	sha  SHA
}

func (g gRef) gqlTypeName() string { return "Ref" }

func (g gRef) gqlField(name string) (any, bool) {
	prefix := "refs/"
	short := strings.TrimPrefix(g.name, "refs/")
	for _, p := range []string{"refs/heads/", "refs/tags/"} {
		if strings.HasPrefix(g.name, p) {
			prefix, short = p, strings.TrimPrefix(g.name, p)
		}
	}
	switch name {
	case "id":
		return base64.RawURLEncoding.EncodeToString([]byte("Ref:" + strconv.FormatInt(g.r.ID, 10) + ":" + g.name)), true
	case "name":
		return short, true
	case "prefix":
		return prefix, true
	case "target":
		if commit := g.r.Objects.Get(g.r.peel(g.sha)).AsCommit(); commit != nil {
			return gCommit{g.c, g.r, commit}, true
		}
		return nil, true
	}
	return nil, false
}

type gCommit struct {
	c      *gqlExec
	r      *Repo
	commit *Commit
}

func (g gCommit) gqlTypeName() string { return "Commit" }

func (g gCommit) gqlField(name string) (any, bool) {
	c := g.commit
	message := apiMessage(c.Message)
	headline, body, _ := strings.Cut(message, "\n")
	switch name {
	case "id":
		return base64.RawURLEncoding.EncodeToString([]byte("Commit:" + strconv.FormatInt(g.r.ID, 10) + ":" + c.ID.String())), true
	case "oid":
		return c.ID.String(), true
	case "abbreviatedOid":
		return c.ID.String()[:7], true
	case "message":
		return message, true
	case "messageHeadline":
		return headline, true
	case "messageBody":
		return strings.TrimLeft(body, "\n"), true
	case "committedDate":
		return formatTime(unixTime(c.Committer.When)), true
	case "authoredDate":
		return formatTime(unixTime(c.Author.When)), true
	case "url":
		return g.c.base + "/" + g.r.FullName() + "/commit/" + c.ID.String(), true
	}
	return nil, false
}

type gLabel struct {
	c *gqlExec
	r *Repo
	l *Label
}

func (g gLabel) gqlTypeName() string { return "Label" }

func (g gLabel) gqlField(name string) (any, bool) {
	switch name {
	case "id":
		return nodeID("Label", g.l.ID), true
	case "name":
		return g.l.Name, true
	case "color":
		return g.l.Color, true
	case "description":
		if g.l.Description == "" {
			return nil, true
		}
		return g.l.Description, true
	case "isDefault":
		return g.l.Default, true
	case "url":
		return g.c.base + "/" + g.r.FullName() + "/labels/" + pathEscape(g.l.Name), true
	}
	return nil, false
}

// gIssue is an issue or a pull request, told apart by Issue.Pull.
type gIssue struct {
	c *gqlExec
	r *Repo
	i *Issue
}

func (g gIssue) gqlTypeName() string {
	if g.i.Pull != nil {
		return "PullRequest"
	}
	return "Issue"
}

func (g gIssue) gqlField(name string) (any, bool) {
	i, r := g.i, g.r
	p := i.Pull
	switch name {
	case "id":
		return nodeID(g.gqlTypeName(), i.ID), true
	case "databaseId":
		return i.ID, true
	case "number":
		return i.Number, true
	case "title":
		return i.Title, true
	case "body", "bodyText":
		return i.Body, true
	case "closed":
		return i.State == "closed", true
	case "closedAt":
		return timeOrNull(i.ClosedAt, formatTime(i.ClosedAt)), true
	case "createdAt":
		return formatTime(i.CreatedAt), true
	case "updatedAt":
		return formatTime(i.UpdatedAt), true
	case "locked":
		return i.Locked, true
	case "author":
		return gUser{g.c, i.User}, true
	case "repository":
		return gRepo{g.c, r}, true
	case "url":
		kind := "issues"
		if p != nil {
			kind = "pull"
		}
		return g.c.base + "/" + r.FullName() + "/" + kind + "/" + strconv.Itoa(i.Number), true
	}
	if p == nil {
		switch name {
		case "state":
			return strings.ToUpper(i.State), true
		case "stateReason":
			if i.StateReason == "" {
				return nil, true
			}
			return strings.ToUpper(i.StateReason), true
		}
		return nil, false
	}
	open := i.State == "open"
	head := r.pullHead(p, open)
	switch name {
	case "state":
		if p.Merged {
			return "MERGED", true
		}
		return strings.ToUpper(i.State), true
	case "merged":
		return p.Merged, true
	case "mergedAt":
		return timeOrNull(p.MergedAt, formatTime(p.MergedAt)), true
	case "isDraft":
		return p.Draft, true
	case "headRefName":
		return p.HeadRef, true
	case "baseRefName":
		return p.BaseRef, true
	case "headRefOid":
		return head.String(), true
	case "baseRefOid":
		return p.BaseSHA.String(), true
	case "mergeable", "mergeStateStatus":
		mergeable, status := "UNKNOWN", "UNKNOWN"
		if open {
			if _, err := r.mergeTrees(r.Branch(p.BaseRef), head); err == nil {
				mergeable, status = "MERGEABLE", "CLEAN"
			} else {
				mergeable, status = "CONFLICTING", "DIRTY"
			}
		}
		if name == "mergeable" {
			return mergeable, true
		}
		return status, true
	case "mergeCommit":
		if commit := r.Objects.Get(p.MergeSHA).AsCommit(); p.Merged && commit != nil {
			return gCommit{g.c, r, commit}, true
		}
		return nil, true
	case "isCrossRepository":
		return false, true
	case "maintainerCanModify":
		return false, true
	case "additions", "deletions", "changedFiles":
		files := r.diffTrees(r.Objects.mergeBase(r.pullBase(p), head), head, false)
		additions, deletions := 0, 0
		for _, f := range files {
			additions += f.Additions
			deletions += f.Deletions
		}
		switch name {
		case "additions":
			return additions, true
		case "deletions":
			return deletions, true
		}
		return len(files), true
	case "headRepository", "baseRepository":
		return gRepo{g.c, r}, true
	case "headRepositoryOwner":
		return gUser{g.c, r.Owner}, true
	}
	return nil, false
}

type gComment struct {
	c       *gqlExec
	r       *Repo
	i       *Issue
	comment *Comment
}

func (g gComment) gqlTypeName() string { return "IssueComment" }

func (g gComment) gqlField(name string) (any, bool) {
	c := g.comment
	switch name {
	case "id":
		return nodeID("IssueComment", c.ID), true
	case "databaseId":
		return c.ID, true
	case "body", "bodyText":
		return c.Body, true
	case "author":
		return gUser{g.c, c.User}, true
	case "createdAt":
		return formatTime(c.CreatedAt), true
	case "updatedAt":
		return formatTime(c.UpdatedAt), true
	case "url":
		kind := "issues"
		if g.i.Pull != nil {
			kind = "pull"
		}
		return g.c.base + "/" + g.r.FullName() + "/" + kind + "/" + strconv.Itoa(g.i.Number) + "#issuecomment-" + strconv.FormatInt(c.ID, 10), true
	case "issue":
		if g.i.Pull != nil {
			return nil, true
		}
		return gIssue{g.c, g.r, g.i}, true
	}
	return nil, false
}

type gChangedFile struct{ f changedFile }

func (g gChangedFile) gqlField(name string) (any, bool) {
	switch name {
	case "path":
		return g.f.Path, true
	case "additions":
		return g.f.Additions, true
	case "deletions":
		return g.f.Deletions, true
	case "changeType":
		switch g.f.Status {
		case "added":
			return "ADDED", true
		case "removed":
			return "DELETED", true
		}
		return "MODIFIED", true
	}
	return nil, false
}

// ---------------------------------------------------------------- connections

// gConn is one page of a connection.
type gConn struct {
	nodes   []any
	offset  int // index of nodes[0] in the whole list
	total   int
	hasNext bool
	hasPrev bool
}

type gEdge struct {
	node   any
	cursor string
}

func (e gEdge) gqlField(name string) (any, bool) {
	switch name {
	case "node":
		return e.node, true
	case "cursor":
		return e.cursor, true
	}
	return nil, false
}

func cursorFor(index int) string {
	return base64.StdEncoding.EncodeToString([]byte("cursor:v2:" + strconv.Itoa(index)))
}

func cursorIndex(cursor string) (int, bool) {
	raw, err := base64.StdEncoding.DecodeString(cursor)
	if err != nil {
		return 0, false
	}
	text, ok := strings.CutPrefix(string(raw), "cursor:v2:")
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(text)
	return n, err == nil
}

// connection pages a whole list by the Relay arguments first/after/last/before.
func connection(items []any, args map[string]any) (*gConn, error) {
	total := len(items)
	lo, hi := 0, total
	if after, ok := args["after"].(string); ok && after != "" {
		index, valid := cursorIndex(after)
		if !valid {
			return nil, &gqlError{Message: "`" + after + "` does not appear to be a valid cursor.", Type: "INVALID_CURSOR_ARGUMENTS"}
		}
		if index+1 > lo {
			lo = index + 1
		}
	}
	if before, ok := args["before"].(string); ok && before != "" {
		index, valid := cursorIndex(before)
		if !valid {
			return nil, &gqlError{Message: "`" + before + "` does not appear to be a valid cursor.", Type: "INVALID_CURSOR_ARGUMENTS"}
		}
		if index < hi {
			hi = index
		}
	}
	if lo > hi {
		lo = hi
	}
	if lo > total {
		lo, hi = total, total
	}
	conn := &gConn{total: total}
	first, hasFirst := args["first"].(int)
	last, hasLast := args["last"].(int)
	switch {
	case hasFirst:
		if first < 0 {
			first = 0
		}
		if hi-lo > first {
			hi = lo + first
			conn.hasNext = true
		}
	case hasLast:
		if last < 0 {
			last = 0
		}
		if hi-lo > last {
			lo = hi - last
			conn.hasPrev = true
		}
	default:
		if hi-lo > 100 {
			hi = lo + 100
			conn.hasNext = true
		}
	}
	if !hasLast && lo > 0 {
		conn.hasPrev = true
	}
	if hasLast && hi < total {
		conn.hasNext = true
	}
	conn.nodes = items[lo:hi]
	conn.offset = lo
	return conn, nil
}

func (c *gConn) gqlField(name string) (any, bool) {
	switch name {
	case "nodes":
		return c.nodes, true
	case "edges":
		edges := make([]any, len(c.nodes))
		for i, node := range c.nodes {
			edges[i] = gEdge{node, cursorFor(c.offset + i)}
		}
		return edges, true
	case "pageInfo":
		var start, end any
		if len(c.nodes) > 0 {
			start = cursorFor(c.offset)
			end = cursorFor(c.offset + len(c.nodes) - 1)
		}
		return om("hasNextPage", c.hasNext, "hasPreviousPage", c.hasPrev, "startCursor", start, "endCursor", end), true
	case "totalCount", "issueCount", "repositoryCount", "userCount":
		return c.total, true
	}
	return nil, false
}

// ---------------------------------------------------------------- lookups by global id

type issueRef struct {
	repo  *Repo
	issue *Issue
}

// findIssueByNode resolves an Issue or PullRequest global id.
func (a *App) findIssueByNode(id string) (issueRef, bool) {
	kind, n, ok := parseNodeID(id)
	if !ok || (kind != "Issue" && kind != "PullRequest") {
		return issueRef{}, false
	}
	for _, repo := range a.repoOrder {
		for _, issue := range repo.Issues {
			if issue.ID == n {
				return issueRef{repo, issue}, true
			}
		}
	}
	return issueRef{}, false
}

func (a *App) findRepoByNode(id string) *Repo {
	kind, n, ok := parseNodeID(id)
	if !ok || kind != "Repository" {
		return nil
	}
	return a.repoByID[n]
}

// nodeByID resolves any global id the forge hands out.
func (c *gqlExec) nodeByID(id string) any {
	a := c.app
	kind, n, ok := parseNodeID(id)
	if !ok {
		return nil
	}
	switch kind {
	case "Repository":
		if repo := a.repoByID[n]; repo != nil {
			return gRepo{c, repo}
		}
	case "Issue", "PullRequest":
		if ref, found := a.findIssueByNode(id); found {
			return gIssue{c, ref.repo, ref.issue}
		}
	case "User":
		for _, u := range a.users {
			if u.ID == n {
				return gUser{c, u}
			}
		}
	case "IssueComment":
		for _, repo := range a.repoOrder {
			for _, issue := range repo.Issues {
				for _, comment := range issue.Comments {
					if comment.ID == n {
						return gComment{c, repo, issue, comment}
					}
				}
			}
		}
	case "Label":
		for _, repo := range a.repoOrder {
			for _, l := range repo.Labels {
				if l.ID == n {
					return gLabel{c, repo, l}
				}
			}
		}
	}
	return nil
}

func notFoundError(what string) *gqlError {
	return &gqlError{Message: "Could not resolve to a node with the global id of '" + what + "'", Type: "NOT_FOUND"}
}

func stringsArg(args map[string]any, key string) []string {
	list, _ := args[key].([]any)
	out := make([]string, 0, len(list))
	for _, item := range list {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func stringArg(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

// ---------------------------------------------------------------- the schema with its resolvers

func buildGraphQLSchema() *gqlSchema {
	s := parseSchema(introspectionSDL + forgeSDL)
	s.installIntrospection()

	// Queries.
	s.attach("Query", "viewer", func(c *gqlExec, p any, args map[string]any) (any, error) {
		return gUser{c, c.app.viewer}, nil
	})
	s.attach("Query", "repository", func(c *gqlExec, p any, args map[string]any) (any, error) {
		repo := c.app.repos[repoKey(stringArg(args, "owner"), stringArg(args, "name"))]
		if repo == nil {
			return nil, nil
		}
		return gRepo{c, repo}, nil
	})
	s.attach("Query", "node", func(c *gqlExec, p any, args map[string]any) (any, error) {
		return c.nodeByID(stringArg(args, "id")), nil
	})
	s.attach("Query", "nodes", func(c *gqlExec, p any, args map[string]any) (any, error) {
		ids := stringsArg(args, "ids")
		out := make([]any, len(ids))
		for i, id := range ids {
			out[i] = c.nodeByID(id)
		}
		return out, nil
	})
	userByLogin := func(c *gqlExec, p any, args map[string]any) (any, error) {
		u := c.app.users[strings.ToLower(stringArg(args, "login"))]
		if u == nil {
			return nil, nil
		}
		return gUser{c, u}, nil
	}
	s.attach("Query", "user", userByLogin)
	s.attach("Query", "repositoryOwner", userByLogin)
	s.attach("Query", "rateLimit", func(c *gqlExec, p any, args map[string]any) (any, error) {
		return om("limit", 5000, "cost", 1, "remaining", 4999, "used", 1, "nodeCount", 1, "resetAt", formatTime(c.app.base.Add(3600e9))), nil
	})
	s.attach("Query", "search", func(c *gqlExec, p any, args map[string]any) (any, error) {
		query := stringArg(args, "query")
		var items []any
		switch stringArg(args, "type") {
		case "ISSUE", "ISSUE_ADVANCED":
			for _, ref := range c.app.searchIssues(query) {
				items = append(items, gIssue{c, ref.repo, ref.issue})
			}
		case "REPOSITORY":
			for _, repo := range c.app.searchRepos(query) {
				items = append(items, gRepo{c, repo})
			}
		case "USER":
			for _, u := range c.app.users {
				if strings.Contains(strings.ToLower(u.Login), strings.ToLower(query)) {
					items = append(items, gUser{c, u})
				}
			}
		}
		return connection(items, args)
	})

	// User.
	s.attach("User", "repositories", func(c *gqlExec, p any, args map[string]any) (any, error) {
		var items []any
		for _, repo := range c.app.repoOrder {
			if repo.Owner == p.(gUser).u {
				items = append(items, gRepo{c, repo})
			}
		}
		return connection(items, args)
	})
	s.attach("User", "repository", func(c *gqlExec, p any, args map[string]any) (any, error) {
		repo := c.app.repos[repoKey(p.(gUser).u.Login, stringArg(args, "name"))]
		if repo == nil {
			return nil, nil
		}
		return gRepo{c, repo}, nil
	})

	// Repository.
	numbered := func(wantPull, either bool) gqlResolveFn {
		return func(c *gqlExec, p any, args map[string]any) (any, error) {
			repo := p.(gRepo).r
			number, _ := args["number"].(int)
			issue := repo.issue(number)
			if issue == nil || (!either && (issue.Pull != nil) != wantPull) {
				return nil, nil
			}
			return gIssue{c, repo, issue}, nil
		}
	}
	s.attach("Repository", "issue", numbered(false, false))
	s.attach("Repository", "pullRequest", numbered(true, false))
	s.attach("Repository", "issueOrPullRequest", numbered(false, true))
	listing := func(pulls bool) gqlResolveFn {
		return func(c *gqlExec, p any, args map[string]any) (any, error) {
			repo := p.(gRepo).r
			f := issueFilter{State: "all", PullsOnly: pulls, NoPulls: !pulls, Direction: "asc", Labels: stringsArg(args, "labels")}
			states := stringsArg(args, "states")
			if filter, ok := args["filterBy"].(map[string]any); ok {
				states = append(states, stringsArg(filter, "states")...)
				f.Labels = append(f.Labels, stringsArg(filter, "labels")...)
				f.Creator = stringArg(filter, "createdBy")
				f.Assignee = stringArg(filter, "assignee")
			}
			if order, ok := args["orderBy"].(map[string]any); ok {
				switch stringArg(order, "field") {
				case "UPDATED_AT":
					f.Sort = "updated"
				case "COMMENTS":
					f.Sort = "comments"
				}
				if stringArg(order, "direction") == "DESC" {
					f.Direction = "desc"
				}
			}
			f.Head = stringArg(args, "headRefName")
			f.Base = stringArg(args, "baseRefName")
			wanted := map[string]bool{}
			for _, state := range states {
				wanted[state] = true
			}
			var items []any
			for _, issue := range repo.filterIssues(f) {
				if len(wanted) > 0 {
					state := strings.ToUpper(issue.State)
					if issue.Pull != nil && issue.Pull.Merged {
						state = "MERGED"
					}
					if !wanted[state] {
						continue
					}
				}
				items = append(items, gIssue{c, repo, issue})
			}
			return connection(items, args)
		}
	}
	s.attach("Repository", "issues", listing(false))
	s.attach("Repository", "pullRequests", listing(true))
	s.attach("Repository", "label", func(c *gqlExec, p any, args map[string]any) (any, error) {
		repo := p.(gRepo).r
		if l := repo.label(stringArg(args, "name")); l != nil {
			return gLabel{c, repo, l}, nil
		}
		return nil, nil
	})
	s.attach("Repository", "labels", func(c *gqlExec, p any, args map[string]any) (any, error) {
		repo := p.(gRepo).r
		query := strings.ToLower(stringArg(args, "query"))
		var items []any
		for _, l := range repo.Labels {
			if query == "" || strings.Contains(strings.ToLower(l.Name), query) {
				items = append(items, gLabel{c, repo, l})
			}
		}
		return connection(items, args)
	})
	s.attach("Repository", "ref", func(c *gqlExec, p any, args map[string]any) (any, error) {
		repo := p.(gRepo).r
		name := stringArg(args, "qualifiedName")
		for _, candidate := range []string{name, "refs/heads/" + name, "refs/tags/" + name} {
			if sha := repo.Ref(candidate); !sha.IsZero() {
				return gRef{c, repo, candidate, sha}, nil
			}
		}
		return nil, nil
	})
	s.attach("Repository", "refs", func(c *gqlExec, p any, args map[string]any) (any, error) {
		repo := p.(gRepo).r
		prefix := stringArg(args, "refPrefix")
		var items []any
		for _, ref := range repo.sortedRefs() {
			if strings.HasPrefix(ref.Name, prefix) {
				items = append(items, gRef{c, repo, ref.Name, ref.SHA})
			}
		}
		return connection(items, args)
	})

	// Issue and PullRequest share their connections.
	labelsOf := func(c *gqlExec, p any, args map[string]any) (any, error) {
		g := p.(gIssue)
		items := make([]any, 0, len(g.i.Labels))
		for _, l := range g.i.Labels {
			items = append(items, gLabel{c, g.r, l})
		}
		return connection(items, args)
	}
	commentsOf := func(c *gqlExec, p any, args map[string]any) (any, error) {
		g := p.(gIssue)
		items := make([]any, 0, len(g.i.Comments))
		for _, comment := range g.i.Comments {
			items = append(items, gComment{c, g.r, g.i, comment})
		}
		return connection(items, args)
	}
	assigneesOf := func(c *gqlExec, p any, args map[string]any) (any, error) {
		g := p.(gIssue)
		items := make([]any, 0, len(g.i.Assignees))
		for _, u := range g.i.Assignees {
			items = append(items, gUser{c, u})
		}
		return connection(items, args)
	}
	for _, typeName := range []string{"Issue", "PullRequest"} {
		s.attach(typeName, "labels", labelsOf)
		s.attach(typeName, "comments", commentsOf)
		s.attach(typeName, "assignees", assigneesOf)
	}
	s.attach("PullRequest", "files", func(c *gqlExec, p any, args map[string]any) (any, error) {
		g := p.(gIssue)
		pull := g.i.Pull
		head := g.r.pullHead(pull, g.i.State == "open")
		files := g.r.diffTrees(g.r.Objects.mergeBase(g.r.pullBase(pull), head), head, false)
		items := make([]any, 0, len(files))
		for _, f := range files {
			items = append(items, gChangedFile{f})
		}
		return connection(items, args)
	})

	// Mutations.
	input := func(args map[string]any) map[string]any {
		in, _ := args["input"].(map[string]any)
		if in == nil {
			in = map[string]any{}
		}
		return in
	}
	payload := func(in map[string]any, key string, value any) *orderedMap {
		return om("clientMutationId", in["clientMutationId"], key, value)
	}
	failed := func(failure *apiError) *gqlError {
		return &gqlError{Message: failure.message, Type: "UNPROCESSABLE"}
	}
	issueFor := func(c *gqlExec, id string, wantPull bool) (issueRef, error) {
		ref, ok := c.app.findIssueByNode(id)
		if !ok || (ref.issue.Pull != nil) != wantPull {
			return issueRef{}, notFoundError(id)
		}
		return ref, nil
	}
	s.attach("Mutation", "createIssue", func(c *gqlExec, p any, args map[string]any) (any, error) {
		in := input(args)
		repo := c.app.findRepoByNode(stringArg(in, "repositoryId"))
		if repo == nil {
			return nil, notFoundError(stringArg(in, "repositoryId"))
		}
		body, hasBody := in["body"].(string)
		req := issueInput{Title: stringArg(in, "title"), Body: body, HasBody: hasBody}
		for _, id := range stringsArg(in, "labelIds") {
			if kind, n, ok := parseNodeID(id); ok && kind == "Label" {
				for _, l := range repo.Labels {
					if l.ID == n {
						req.Labels = append(req.Labels, l.Name)
					}
				}
			}
		}
		issue, failure := c.app.createIssue(repo, req)
		if failure != nil {
			return nil, failed(failure)
		}
		return payload(in, "issue", gIssue{c, repo, issue}), nil
	})
	s.attach("Mutation", "updateIssue", func(c *gqlExec, p any, args map[string]any) (any, error) {
		in := input(args)
		ref, err := issueFor(c, stringArg(in, "id"), false)
		if err != nil {
			return nil, err
		}
		if title, ok := in["title"].(string); ok && strings.TrimSpace(title) != "" {
			ref.issue.Title = title
		}
		if body, ok := in["body"].(string); ok {
			ref.issue.Body, ref.issue.HasBody = body, true
		}
		if state, ok := in["state"].(string); ok {
			c.app.setIssueState(ref.issue, strings.ToLower(state), "")
		} else {
			ref.issue.UpdatedAt = c.app.now()
		}
		ref.issue.cached.Store(nil)
		return payload(in, "issue", gIssue{c, ref.repo, ref.issue}), nil
	})
	setState := func(key string, wantPull bool, state string) gqlResolveFn {
		return func(c *gqlExec, p any, args map[string]any) (any, error) {
			in := input(args)
			ref, err := issueFor(c, stringArg(in, key), wantPull)
			if err != nil {
				return nil, err
			}
			if wantPull && ref.issue.Pull.Merged && state == "open" {
				return nil, &gqlError{Message: "Could not open the pull request.", Type: "UNPROCESSABLE"}
			}
			if wantPull && state == "closed" && ref.issue.State == "open" {
				ref.issue.Pull.HeadSHA = ref.repo.pullHead(ref.issue.Pull, true)
			}
			c.app.setIssueState(ref.issue, state, strings.ToLower(stringArg(in, "stateReason")))
			name := "issue"
			if wantPull {
				name = "pullRequest"
			}
			return payload(in, name, gIssue{c, ref.repo, ref.issue}), nil
		}
	}
	s.attach("Mutation", "closeIssue", setState("issueId", false, "closed"))
	s.attach("Mutation", "reopenIssue", setState("issueId", false, "open"))
	s.attach("Mutation", "closePullRequest", setState("pullRequestId", true, "closed"))
	s.attach("Mutation", "reopenPullRequest", setState("pullRequestId", true, "open"))
	s.attach("Mutation", "addComment", func(c *gqlExec, p any, args map[string]any) (any, error) {
		in := input(args)
		ref, ok := c.app.findIssueByNode(stringArg(in, "subjectId"))
		if !ok {
			return nil, notFoundError(stringArg(in, "subjectId"))
		}
		body := stringArg(in, "body")
		if body == "" {
			return nil, &gqlError{Message: "Body cannot be blank", Type: "UNPROCESSABLE"}
		}
		comment := c.app.addComment(ref.repo, ref.issue, body)
		edge := gEdge{gComment{c, ref.repo, ref.issue, comment}, cursorFor(len(ref.issue.Comments) - 1)}
		out := payload(in, "commentEdge", edge)
		out.set("subject", gIssue{c, ref.repo, ref.issue})
		return out, nil
	})
	s.attach("Mutation", "createPullRequest", func(c *gqlExec, p any, args map[string]any) (any, error) {
		in := input(args)
		repo := c.app.findRepoByNode(stringArg(in, "repositoryId"))
		if repo == nil {
			return nil, notFoundError(stringArg(in, "repositoryId"))
		}
		body, hasBody := in["body"].(string)
		draft, _ := in["draft"].(bool)
		issue, failure := c.app.createPull(repo, pullInput{
			Title: stringArg(in, "title"), Body: body, HasBody: hasBody,
			Head: stringArg(in, "headRefName"), Base: stringArg(in, "baseRefName"), Draft: draft,
		})
		if failure != nil {
			return nil, failed(failure)
		}
		return payload(in, "pullRequest", gIssue{c, repo, issue}), nil
	})
	s.attach("Mutation", "updatePullRequest", func(c *gqlExec, p any, args map[string]any) (any, error) {
		in := input(args)
		ref, err := issueFor(c, stringArg(in, "pullRequestId"), true)
		if err != nil {
			return nil, err
		}
		if title, ok := in["title"].(string); ok && strings.TrimSpace(title) != "" {
			ref.issue.Title = title
		}
		if body, ok := in["body"].(string); ok {
			ref.issue.Body, ref.issue.HasBody = body, true
		}
		if base, ok := in["baseRefName"].(string); ok && base != "" && !ref.issue.Pull.Merged {
			if tip := ref.repo.Branch(base); !tip.IsZero() {
				ref.issue.Pull.BaseRef, ref.issue.Pull.BaseSHA = base, tip
			}
		}
		if state, ok := in["state"].(string); ok && !ref.issue.Pull.Merged {
			c.app.setIssueState(ref.issue, strings.ToLower(state), "")
		} else {
			ref.issue.UpdatedAt = c.app.now()
		}
		return payload(in, "pullRequest", gIssue{c, ref.repo, ref.issue}), nil
	})
	s.attach("Mutation", "mergePullRequest", func(c *gqlExec, p any, args map[string]any) (any, error) {
		in := input(args)
		ref, err := issueFor(c, stringArg(in, "pullRequestId"), true)
		if err != nil {
			return nil, err
		}
		method := strings.ToLower(stringArg(in, "mergeMethod"))
		if _, failure := c.app.mergePull(ref.repo, ref.issue, method, stringArg(in, "commitHeadline"), stringArg(in, "commitBody"), stringArg(in, "expectedHeadOid")); failure != nil {
			return nil, failed(failure)
		}
		return payload(in, "pullRequest", gIssue{c, ref.repo, ref.issue}), nil
	})
	s.attach("Mutation", "createRepository", func(c *gqlExec, p any, args map[string]any) (any, error) {
		in := input(args)
		description, hasDesc := in["description"].(string)
		repo, failure := c.app.newRepo(stringArg(in, "name"), description, hasDesc, stringArg(in, "visibility") != "PUBLIC", false)
		if failure != nil {
			return nil, &gqlError{Message: "Name already exists on this account", Type: "UNPROCESSABLE"}
		}
		if homepage := stringArg(in, "homepageUrl"); homepage != "" {
			repo.Homepage = homepage
		}
		if enabled, ok := in["hasIssuesEnabled"].(bool); ok {
			repo.HasIssues = enabled
		}
		return payload(in, "repository", gRepo{c, repo}), nil
	})
	return s
}

// ---------------------------------------------------------------- HTTP

type graphQLRequest struct {
	Query         *string        `json:"query"`
	OperationName *string        `json:"operationName"`
	Variables     map[string]any `json:"variables"`
}

func (a *App) handleGraphQL(req *Request) *Response {
	if ok, present := a.authorized(req); !ok {
		return unauthorized(present)
	}
	if req.Method != "POST" {
		return jsonMessage(405, "GraphQL only answers POST")
	}
	trimmed := strings.TrimSpace(string(req.Body))
	if trimmed == "" {
		return jsonMessage(400, "Problems parsing JSON")
	}
	base := baseURL(req)
	run := func(raw json.RawMessage) []byte {
		var one graphQLRequest
		if err := json.Unmarshal(raw, &one); err != nil || one.Query == nil {
			return (&gqlResult{Errors: []*gqlError{{Message: "A query attribute must be specified and must be a string."}}}).toJSON()
		}
		name := ""
		if one.OperationName != nil {
			name = *one.OperationName
		}
		return runGraphQL(a, a.schema, base, *one.Query, name, one.Variables).toJSON()
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if trimmed[0] == '[' {
		var batch []json.RawMessage
		if err := json.Unmarshal(req.Body, &batch); err != nil {
			return jsonMessage(400, "Problems parsing JSON")
		}
		out := make([]byte, 0, 1024)
		out = append(out, '[')
		for i, raw := range batch {
			if i > 0 {
				out = append(out, ',')
			}
			out = append(out, run(raw)...)
		}
		out = append(out, ']')
		return &Response{Status: 200, ContentType: jsonType, Body: out}
	}
	if !json.Valid(req.Body) {
		return jsonMessage(400, "Problems parsing JSON")
	}
	return &Response{Status: 200, ContentType: jsonType, Body: run(req.Body)}
}
