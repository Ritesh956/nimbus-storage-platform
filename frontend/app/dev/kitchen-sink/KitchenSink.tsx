"use client";

import { useState } from "react";
import { Button } from "@/components/ui/Button";
import { Badge } from "@/components/ui/Badge";
import { Input } from "@/components/ui/Input";
import { Card, EyebrowLabel, StatCard } from "@/components/ui/Card";
import { Checkbox } from "@/components/ui/Checkbox";
import { PageHeader } from "@/components/ui/PageHeader";
import { TablePanel, Th, Td, Tr } from "@/components/ui/Table";
import { EmptyState } from "@/components/ui/EmptyState";
import { ThemeToggle } from "@/components/ThemeToggle";
import * as Icons from "@/components/ui/Icons";

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="flex flex-col gap-3">
      <h2 className="text-sm font-semibold uppercase tracking-wider text-muted-2">{title}</h2>
      <div className="panel flex flex-col gap-4 p-5">{children}</div>
    </section>
  );
}

function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex flex-wrap items-center gap-3">
      <span className="w-28 shrink-0 text-xs text-muted-2">{label}</span>
      <div className="flex flex-wrap items-center gap-3">{children}</div>
    </div>
  );
}

// Every named export from components/ui/Icons.tsx that's a plain icon
// component (SVGProps in, <svg> out) — LogoMark has a different signature
// (it's a composed brand mark, not a line icon) so it's shown separately.
const iconEntries = Object.entries(Icons).filter(([name]) => name.endsWith("Icon")) as [
  string,
  (p: { size?: number }) => React.ReactElement,
][];

export function KitchenSink() {
  const [checked, setChecked] = useState(true);
  const [selected, setSelected] = useState<string | null>(null);

  return (
    <div className="mx-auto flex max-w-4xl flex-col gap-8 px-4 py-8 sm:px-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold tracking-tight">Kitchen sink</h1>
          <p className="mt-1 text-xs text-muted">
            Every <code className="text-muted-2">components/ui/</code> primitive, every state. Dev-only —
            see this route&apos;s own guard. Toggle the theme to check both.
          </p>
        </div>
        <ThemeToggle />
      </div>

      <Section title="Button — 4 variants × default/disabled">
        <Row label="primary">
          <Button variant="primary">Save</Button>
          <Button variant="primary" disabled>
            Save
          </Button>
        </Row>
        <Row label="secondary">
          <Button variant="secondary">Cancel</Button>
          <Button variant="secondary" disabled>
            Cancel
          </Button>
        </Row>
        <Row label="ghost">
          <Button variant="ghost">Dismiss</Button>
          <Button variant="ghost" disabled>
            Dismiss
          </Button>
        </Row>
        <Row label="danger">
          <Button variant="danger">Delete</Button>
          <Button variant="danger" disabled>
            Delete
          </Button>
        </Row>
      </Section>

      <Section title="Badge — 4 tones">
        <Row label="tones">
          <Badge tone="neutral">Neutral</Badge>
          <Badge tone="success">Healthy</Badge>
          <Badge tone="danger">Down</Badge>
          <Badge tone="warning">Degraded</Badge>
        </Row>
      </Section>

      <Section title="Input">
        <Row label="empty">
          <Input placeholder="Folder name" className="max-w-56" />
        </Row>
        <Row label="filled">
          <Input defaultValue="Q3 reports" className="max-w-56" />
        </Row>
        <Row label="disabled">
          <Input defaultValue="Read-only" disabled className="max-w-56" />
        </Row>
      </Section>

      <Section title="Checkbox">
        <Row label="interactive">
          <label className="flex cursor-pointer items-center gap-2 text-sm">
            <Checkbox checked={checked} onChange={() => setChecked((c) => !c)} />
            {checked ? "Checked" : "Unchecked"} (click to toggle)
          </label>
        </Row>
        <Row label="disabled">
          <span className="flex items-center gap-2 text-sm text-muted-2">
            <Checkbox checked readOnly disabled />
            Checked, disabled
          </span>
          <span className="flex items-center gap-2 text-sm text-muted-2">
            <Checkbox checked={false} readOnly disabled />
            Unchecked, disabled
          </span>
        </Row>
      </Section>

      <Section title="Card, EyebrowLabel, StatCard">
        <Row label="Card">
          <Card className="max-w-xs">
            <EyebrowLabel>Storage</EyebrowLabel>
            <p className="mt-1 text-sm">Plain panel — the base every other surface sits on.</p>
          </Card>
        </Row>
        <Row label="StatCard">
          <StatCard label="Nodes healthy" value="3 / 3" chip="all up" chipTone="success" />
          <StatCard label="Dead events" value="2" chip="attention" chipTone="danger" />
          <StatCard label="Org usage" value="4.2 GB" chipTone="neutral" chip="of 10 GB" />
        </Row>
      </Section>

      <Section title="PageHeader">
        <div className="rounded-lg border border-border/50 p-3">
          <PageHeader title="Files" description="With a description and an action." actions={<Button variant="secondary">New folder</Button>} />
        </div>
        <div className="rounded-lg border border-border/50 p-3">
          <PageHeader title="Minimal — title only" />
        </div>
      </Section>

      <Section title="Table (TablePanel / Th / Td / Tr)">
        <TablePanel title="Storage nodes">
          <thead>
            <tr>
              <Th>Node</Th>
              <Th>Status</Th>
              <Th>Latency</Th>
            </tr>
          </thead>
          <tbody>
            {[
              { id: "node-1", status: "healthy", latency: "1.2ms" },
              { id: "node-2", status: "healthy", latency: "1.6ms" },
              { id: "node-3", status: "down", latency: "—" },
            ].map((n) => (
              <Tr key={n.id} onClick={() => setSelected(n.id)} className={selected === n.id ? "bg-surface-2/60" : ""}>
                <Td className="font-medium">{n.id}</Td>
                <Td>
                  <Badge tone={n.status === "healthy" ? "success" : "danger"}>{n.status}</Badge>
                </Td>
                <Td className="text-muted-2">{n.latency}</Td>
              </Tr>
            ))}
          </tbody>
        </TablePanel>
        <p className="text-[11px] text-muted-2">Rows are clickable (Tr with onClick) — try Tab + Enter, not just a mouse click.</p>
      </Section>

      <Section title="EmptyState">
        <Row label="with action">
          <div className="w-full max-w-sm rounded-lg border border-border/50">
            <EmptyState
              icon={<Icons.FileIcon size={18} />}
              title="No files here yet"
              description="Drop one on the dropzone above, or drag it in from your desktop."
              action={<Button variant="secondary">Upload</Button>}
            />
          </div>
        </Row>
        <Row label="no action">
          <div className="w-full max-w-sm rounded-lg border border-border/50">
            <EmptyState icon={<Icons.SearchIcon size={18} />} title="No results" description="Try a different query or filter." />
          </div>
        </Row>
      </Section>

      <Section title={`Icons (${iconEntries.length})`}>
        <div className="grid grid-cols-4 gap-4 sm:grid-cols-6 md:grid-cols-8">
          {iconEntries.map(([name, Icon]) => (
            <div key={name} className="flex flex-col items-center gap-1.5 rounded-lg border border-border/40 p-3">
              <Icon size={18} />
              <span className="text-center text-[10px] leading-tight text-muted-2">{name}</span>
            </div>
          ))}
        </div>
      </Section>
    </div>
  );
}
