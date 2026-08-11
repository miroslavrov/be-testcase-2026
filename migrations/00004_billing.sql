-- +goose Up
create table usage_records (
    id uuid primary key default gen_random_uuid(),
    org_id uuid not null references organizations(id),
    task_id uuid not null references tasks(id),
    -- uuid колла уникален, чтобы не списать дважды если воркер перезапустил call после падения
    tool_call_id uuid not null unique references tool_calls(id),
    cost_usd numeric(12, 2) not null,
    recorded_at timestamptz not null default now()
);

create index usage_records_org_period_idx on usage_records (org_id, recorded_at);

create table invoices (
    id uuid primary key default gen_random_uuid(),
    org_id uuid not null references organizations(id),
    period_start date not null,
    period_end date not null,
    total_usd numeric(12, 2) not null,
    line_count int not null default 0,
    status text not null default 'issued' check (status in ('issued', 'paid', 'void')),
    created_at timestamptz not null default now(),
    -- один инвойс на оргу и период, гонка двух генераторов разруливается прямо тут
    unique (org_id, period_start)
);

create table idempotency_keys (
    org_id uuid not null references organizations(id),
    key text not null,
    request_hash text not null,
    response_status int,
    response_body jsonb,
    created_at timestamptz not null default now(),
    primary key (org_id, key)
);

-- +goose Down
drop table idempotency_keys;
drop table invoices;
drop table usage_records;
