const roleStyles: Record<string, { bg: string; fg: string }> = {
  council: { bg: "var(--color-role-council-bg)", fg: "var(--color-role-council)" },
  moderator: { bg: "var(--color-role-moderator-bg)", fg: "var(--color-role-moderator)" },
  member: { bg: "var(--color-role-member-bg)", fg: "var(--color-role-member)" },
  pending: { bg: "var(--color-role-pending-bg)", fg: "var(--color-role-pending)" },
  banned: { bg: "var(--color-role-banned-bg)", fg: "var(--color-role-banned)" },
};

export default function RoleBadge({ role }: { role: string }) {
  const styles = roleStyles[role] ?? roleStyles.pending;
  return (
    <span
      className="inline-block rounded-full px-2.5 py-0.5 text-xs font-semibold capitalize"
      style={{ backgroundColor: styles.bg, color: styles.fg }}
    >
      {role}
    </span>
  );
}
