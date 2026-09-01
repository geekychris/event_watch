// Cheatsheet examples — kept in sync with docs/cheatsheet.md. Every row is
// one canned publish: click "Inject →" in the CheatsheetCard and the Publish
// card fields fill in with these exact values. Keeps the docs and the UI
// literally one source of truth.

export interface Example {
  group: string;              // "pr" | "build" | "deploy" | "job" | "chat" | "int" | "str" | "time"
  label: string;              // human-friendly, e.g. "pr_opened — new PR"
  topic: string;
  type: string;
  payload: Record<string, unknown>;
  note?: string;              // one-liner shown as a hint
}

export const EXAMPLES: Example[] = [
  // -- pr --
  { group: 'pr', label: 'pr_opened — new PR', topic: 'pr/octo/hello/1', type: 'pr_opened',
    payload: { title: 'Add feature X', author: 'alice', base: 'main', head: 'abc123' } },
  { group: 'pr', label: 'pr_review_requested', topic: 'pr/octo/hello/1', type: 'pr_review_requested',
    payload: { reviewer: 'bob' } },
  { group: 'pr', label: 'pr_reviewed — approved', topic: 'pr/octo/hello/1', type: 'pr_reviewed',
    payload: { state: 'approved' } },
  { group: 'pr', label: 'pr_reviewed — changes requested', topic: 'pr/octo/hello/1', type: 'pr_reviewed',
    payload: { state: 'changes_requested' } },
  { group: 'pr', label: 'check_run_completed — success', topic: 'pr/octo/hello/1', type: 'check_run_completed',
    payload: { conclusion: 'success', name: 'unit-tests' } },
  { group: 'pr', label: 'check_run_completed — failure', topic: 'pr/octo/hello/1', type: 'check_run_completed',
    payload: { conclusion: 'failure', name: 'unit-tests' } },
  { group: 'pr', label: 'pr_commented', topic: 'pr/octo/hello/1', type: 'pr_commented', payload: {} },
  { group: 'pr', label: 'pr_merged', topic: 'pr/octo/hello/1', type: 'pr_merged', payload: {} },
  { group: 'pr', label: 'pr_closed (not merged)', topic: 'pr/octo/hello/1', type: 'pr_closed', payload: {} },

  // -- build --
  { group: 'build', label: 'build_queued', topic: 'build/ci/42', type: 'build_queued', payload: {} },
  { group: 'build', label: 'build_started', topic: 'build/ci/42', type: 'build_started', payload: {} },
  { group: 'build', label: 'step_started', topic: 'build/ci/42', type: 'step_started', payload: { step: 'compile' } },
  { group: 'build', label: 'step_finished — success', topic: 'build/ci/42', type: 'step_finished',
    payload: { step: 'compile', status: 'success' } },
  { group: 'build', label: 'step_finished — failed', topic: 'build/ci/42', type: 'step_finished',
    payload: { step: 'compile', status: 'failed' } },
  { group: 'build', label: 'build_finished — success', topic: 'build/ci/42', type: 'build_finished',
    payload: { status: 'success' } },

  // -- deploy --
  { group: 'deploy', label: 'deploy_started v42', topic: 'deploy/prod/api', type: 'deploy_started',
    payload: { version: 'v42', env: 'prod', service: 'api' } },
  { group: 'deploy', label: 'health_check_pass', topic: 'deploy/prod/api', type: 'health_check_pass', payload: {} },
  { group: 'deploy', label: 'health_check_fail', topic: 'deploy/prod/api', type: 'health_check_fail', payload: {} },
  { group: 'deploy', label: 'rollback → v41', topic: 'deploy/prod/api', type: 'rollback', payload: { to: 'v41' } },
  { group: 'deploy', label: 'deploy_finished', topic: 'deploy/prod/api', type: 'deploy_finished',
    payload: { status: 'success' } },

  // -- job --
  { group: 'job', label: 'job_started', topic: 'job/reindex-1', type: 'job_started', payload: { name: 'reindex' } },
  { group: 'job', label: 'job_progress 50%', topic: 'job/reindex-1', type: 'job_progress',
    payload: { percent: 50, eta_seconds: 30 } },
  { group: 'job', label: 'job_log', topic: 'job/reindex-1', type: 'job_log', payload: { line: 'processing shard 3' } },
  { group: 'job', label: 'job_finished', topic: 'job/reindex-1', type: 'job_finished', payload: {} },
  { group: 'job', label: 'job_failed', topic: 'job/reindex-1', type: 'job_failed', payload: {} },

  // -- chat --
  { group: 'chat', label: 'user_joined', topic: 'chat/general', type: 'user_joined', payload: { user: 'alice' } },
  { group: 'chat', label: 'msg_posted', topic: 'chat/general', type: 'msg_posted',
    payload: { id: 'm1', user: 'alice', text: 'hey team' } },
  { group: 'chat', label: 'msg_edited', topic: 'chat/general', type: 'msg_edited', payload: { id: 'm1', text: 'hey team!' } },
  { group: 'chat', label: 'msg_deleted', topic: 'chat/general', type: 'msg_deleted', payload: { id: 'm1' } },
  { group: 'chat', label: 'user_left', topic: 'chat/general', type: 'user_left', payload: { user: 'alice' } },

  // -- str --
  { group: 'str', label: 'str_set "alice"', topic: 'str/name', type: 'str_set', payload: { value: 'alice' } },
  { group: 'str', label: 'str_set "" (empty is still "set")', topic: 'str/name', type: 'str_set',
    payload: { value: '' }, note: 'value="" and exists=true — different from delete' },
  { group: 'str', label: 'str_delete', topic: 'str/name', type: 'str_delete', payload: {},
    note: 'value="" and exists=false' },

  // -- int (math ops) --
  { group: 'int', label: 'int_set 100', topic: 'int/counter', type: 'int_set', payload: { value: 100 } },
  { group: 'int', label: 'int_incr +1 (default)', topic: 'int/counter', type: 'int_incr', payload: {},
    note: 'no delta key → defaults to +1' },
  { group: 'int', label: 'int_incr +5', topic: 'int/counter', type: 'int_incr', payload: { delta: 5 } },
  { group: 'int', label: 'int_incr -3 (negative delta subtracts)', topic: 'int/counter', type: 'int_incr',
    payload: { delta: -3 }, note: 'negative delta on incr works — subtracts 3' },
  { group: 'int', label: 'int_decr -1 (default)', topic: 'int/counter', type: 'int_decr', payload: {},
    note: 'no delta key → defaults to -1' },
  { group: 'int', label: 'int_decr +5', topic: 'int/counter', type: 'int_decr', payload: { delta: 5 } },
  { group: 'int', label: 'int_incr from unset (starts at 0)', topic: 'int/hits', type: 'int_incr', payload: { delta: 7 },
    note: 'no need to int_set 0 first — incr on unset starts from 0' },
  { group: 'int', label: 'int_set 0 (explicit zero, exists=true)', topic: 'int/counter', type: 'int_set',
    payload: { value: 0 }, note: 'different from int_delete: exists=true' },
  { group: 'int', label: 'int_delete (value=0, exists=false)', topic: 'int/counter', type: 'int_delete', payload: {},
    note: 'unset it entirely — different from setting to 0' },

  // -- time (math ops) --
  { group: 'time', label: 'time_now (server clock)', topic: 'time/last_seen', type: 'time_now', payload: {},
    note: 'uses server\'s clock — no client skew' },
  { group: 'time', label: 'time_set RFC3339', topic: 'time/last_seen', type: 'time_set',
    payload: { value: '2026-09-01T12:00:00Z' } },
  { group: 'time', label: 'time_add +1h (3600s)', topic: 'time/last_seen', type: 'time_add', payload: { seconds: 3600 } },
  { group: 'time', label: 'time_add -60s (subtracts)', topic: 'time/last_seen', type: 'time_add',
    payload: { seconds: -60 }, note: 'negative seconds subtract' },
  { group: 'time', label: 'time_delete', topic: 'time/last_seen', type: 'time_delete', payload: {} },
];

export const GROUPS: { key: string; label: string; hint: string }[] = [
  { key: 'pr',     label: 'PR',      hint: 'GitHub-style pull-request lifecycle' },
  { key: 'build',  label: 'Build',   hint: 'CI build + step timings' },
  { key: 'deploy', label: 'Deploy',  hint: 'Deployment with versions + health' },
  { key: 'job',    label: 'Job',     hint: 'Long-running job with progress + log tail' },
  { key: 'chat',   label: 'Chat',    hint: 'Chat room with participants + messages' },
  { key: 'str',    label: 'String',  hint: 'Scalar string field' },
  { key: 'int',    label: 'Int',     hint: 'Scalar integer field with atomic incr/decr' },
  { key: 'time',   label: 'Time',    hint: 'Scalar timestamp field with add/sub' },
];
