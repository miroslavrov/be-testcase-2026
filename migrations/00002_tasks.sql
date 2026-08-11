-- +goose Up
create table agent_slots (
    id uuid primary key default gen_random_uuid(),
    org_id uuid not null references organizations(id),
    slot_type text not null check (slot_type in ('standard', 'fast')),
    status text not null default 'available' check (status in ('available', 'busy')),
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);

-- под запрос резервирования: свободный слот нужного типа
create index agent_slots_free_idx on agent_slots (org_id, slot_type) where status = 'available';
-- подсчёт занятых при проверке лимита плана
create index agent_slots_busy_idx on agent_slots (org_id) where status = 'busy';

create table tool_definitions (
    id uuid primary key default gen_random_uuid(),
    name text not null unique,
    risk_level text not null check (risk_level in ('read', 'write', 'destructive')),
    base_cost_usd numeric(12, 2) not null default 0,
    mock_min_ms int not null default 100,
    mock_max_ms int not null default 1500,
    mock_failure_rate numeric(3, 2) not null default 0.05 check (mock_failure_rate between 0 and 1),
    created_at timestamptz not null default now()
);

create table tasks (
    id uuid primary key default gen_random_uuid(),
    org_id uuid not null references organizations(id),
    created_by uuid not null references users(id),
    title text not null,
    required_slot_type text not null check (required_slot_type in ('standard', 'fast')),
    priority int not null default 3 check (priority between 1 and 5),
    status text not null default 'submitted' check (status in ('submitted', 'queued', 'running', 'awaiting_approval', 'completed', 'failed', 'cancelled')),
    estimated_cost_usd numeric(12, 2) not null default 0,
    failure_reason text,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);

-- списки в апи всегда по орге и статусу
create index tasks_org_status_idx on tasks (org_id, status, created_at desc);
-- очередь: воркеры берут queued по приоритету
create index tasks_queue_idx on tasks (priority desc, created_at) where status = 'queued';
-- сумма оценок активных задач при проверке бюджета
create index tasks_active_estimate_idx on tasks (org_id) include (estimated_cost_usd)
    where status in ('submitted', 'queued', 'running', 'awaiting_approval');

create table tool_calls (
    id uuid primary key default gen_random_uuid(),
    task_id uuid not null references tasks(id),
    org_id uuid not null references organizations(id),
    tool_id uuid not null references tool_definitions(id),
    order_index int not null,
    input_params jsonb not null default '{}',
    estimated_cost_usd numeric(12, 2) not null default 0,
    status text not null default 'pending' check (status in ('pending', 'awaiting_approval', 'executing', 'completed', 'failed', 'rejected')),
    actual_cost_usd numeric(12, 2),
    result jsonb,
    started_at timestamptz,
    finished_at timestamptz,
    unique (task_id, order_index)
);

create table task_executions (
    id uuid primary key default gen_random_uuid(),
    task_id uuid not null unique references tasks(id),
    org_id uuid not null references organizations(id),
    slot_id uuid not null references agent_slots(id),
    status text not null default 'running' check (status in ('running', 'awaiting_approval', 'completed', 'failed', 'cancelled')),
    current_call_index int not null default 0,
    lease_expires_at timestamptz not null,
    started_at timestamptz not null default now(),
    finished_at timestamptz,
    updated_at timestamptz not null default now()
);

-- воркеры ищут running с протухшим лизом: тут и упавшие воркеры, и возобновление после аппрува
create index task_executions_claim_idx on task_executions (lease_expires_at) where status = 'running';

create table state_transitions (
    id bigint generated always as identity primary key,
    org_id uuid not null,
    entity_type text not null,
    entity_id uuid not null,
    from_status text,
    to_status text not null,
    actor_type text not null default 'system',
    actor_id uuid,
    created_at timestamptz not null default now()
);

create index state_transitions_entity_idx on state_transitions (entity_type, entity_id, id);

-- +goose Down
drop table state_transitions;
drop table task_executions;
drop table tool_calls;
drop table tasks;
drop table tool_definitions;
drop table agent_slots;
