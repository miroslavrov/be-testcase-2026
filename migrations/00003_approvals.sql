-- +goose Up
create table approval_chains (
    id uuid primary key default gen_random_uuid(),
    org_id uuid not null references organizations(id),
    risk_level text not null check (risk_level in ('write', 'destructive')),
    created_at timestamptz not null default now(),
    unique (org_id, risk_level)
);

create table approval_chain_steps (
    id uuid primary key default gen_random_uuid(),
    chain_id uuid not null references approval_chains(id) on delete cascade,
    step_order int not null,
    approver_role text not null check (approver_role in ('owner', 'admin', 'approver')),
    timeout_hours numeric(6, 2) not null check (timeout_hours > 0),
    on_timeout text not null default 'reject' check (on_timeout in ('escalate', 'reject')),
    unique (chain_id, step_order)
);

create table approval_requests (
    id uuid primary key default gen_random_uuid(),
    org_id uuid not null references organizations(id),
    tool_call_id uuid not null unique references tool_calls(id),
    chain_id uuid not null references approval_chains(id),
    current_step_order int not null,
    current_step_deadline timestamptz not null,
    status text not null default 'pending' check (status in ('pending', 'approved', 'rejected')),
    created_at timestamptz not null default now(),
    resolved_at timestamptz
);

-- инбокс согласующего
create index approval_requests_pending_idx on approval_requests (org_id, created_at) where status = 'pending';
-- свип таймаутов
create index approval_requests_deadline_idx on approval_requests (current_step_deadline) where status = 'pending';

create table approval_decisions (
    id uuid primary key default gen_random_uuid(),
    request_id uuid not null references approval_requests(id),
    step_order int not null,
    approver_id uuid references users(id),
    decision text not null check (decision in ('approved', 'rejected', 'timeout_escalated', 'timeout_rejected')),
    comment text,
    created_at timestamptz not null default now()
);

create index approval_decisions_request_idx on approval_decisions (request_id, created_at);

create table notifications (
    id uuid primary key default gen_random_uuid(),
    org_id uuid not null references organizations(id),
    user_id uuid references users(id),
    kind text not null,
    payload jsonb not null default '{}',
    created_at timestamptz not null default now()
);

-- +goose Down
drop table notifications;
drop table approval_decisions;
drop table approval_requests;
drop table approval_chain_steps;
drop table approval_chains;
