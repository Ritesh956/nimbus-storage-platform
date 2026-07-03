/* Dashdark X table pattern: a panel wrapping a full-width table — muted
   uppercase-ish header row, hairline row separators, rows lighten on hover.
   Shared by the files/search/trash/activity/admin pages so they all read as
   one product. */
export function TablePanel({
  title,
  children,
  className = "",
}: {
  title?: string;
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <div className={`panel overflow-hidden ${className}`}>
      {title && (
        <div className="border-b border-border/60 px-5 py-3.5 text-sm font-medium">{title}</div>
      )}
      <div className="overflow-x-auto">
        <table className="w-full text-left text-sm">{children}</table>
      </div>
    </div>
  );
}

export function Th({ children, className = "" }: { children?: React.ReactNode; className?: string }) {
  return (
    <th className={`px-5 py-3 text-[11px] font-semibold uppercase tracking-wider text-muted-2 ${className}`}>
      {children}
    </th>
  );
}

export function Td({ children, className = "" }: { children?: React.ReactNode; className?: string }) {
  return <td className={`px-5 py-3 align-middle ${className}`}>{children}</td>;
}

export function Tr({
  children,
  className = "",
  onClick,
}: {
  children: React.ReactNode;
  className?: string;
  onClick?: () => void;
}) {
  return (
    <tr
      onClick={onClick}
      className={`border-t border-border/40 transition-colors hover:bg-surface-2/60 ${onClick ? "cursor-pointer" : ""} ${className}`}
    >
      {children}
    </tr>
  );
}
