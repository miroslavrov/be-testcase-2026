-- +goose Up
create table organizations (
    id uuid primary key default gen_random_uuid(),
    name text not null,
    created_at timestamptz not null default now()
);

create table users (
    id uuid primary key default gen_random_uuid(),
    org_id uuid not null references organizations(id),
    email text not null unique,
    password_hash text not null,
    role text not null check (role in ('owner', 'admin', 'approver', 'member')),
    created_at timestamptz not null default now()
);

create index users_org_idx on users (org_id);

create table plans (
    id uuid primary key default gen_random_uuid(),
    name text not null unique,
    max_concurrent_slots int not null check (max_concurrent_slots > 0),
    monthly_budget_usd numeric(12, 2) not null check (monthly_budget_usd >= 0),
    auto_approve_threshold_usd numeric(12, 2) not null default 0,
    created_at timestamptz not null default now()
);

create table subscriptions (
    id uuid primary key default gen_random_uuid(),
    org_id uuid not null references organizations(id),
    plan_id uuid not null references plans(id),
    status text not null check (status in ('active', 'cancelled', 'expired')),
    current_period_start timestamptz not null,
    current_period_end timestamptz not null,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);

-- не больше одной активной подписки на организацию
create unique index subscriptions_one_active_idx on subscriptions (org_id) where status = 'active';

-- +goose Down
drop table subscriptions;
drop table plans;
drop table users;
drop table organizations;
