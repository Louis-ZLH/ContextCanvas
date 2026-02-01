import React from "react";

interface NavItemProps {
  icon: React.ReactNode;
  label: string;
  active?: boolean;
}

export function NavItem({ icon, label, active = false }: NavItemProps) {
  return (
    <a
      href="#"
      className="flex items-center px-3 py-2 rounded-md transition-colors gap-3"
      style={{
        backgroundColor: active ? "var(--accent-light)" : "transparent",
        color: active ? "var(--accent)" : "var(--text-primary)",
        fontWeight: active ? 500 : 400,
      }}
    >
      <span
        style={{
          color: active ? "var(--accent)" : "var(--text-secondary)",
        }}
      >
        {icon}
      </span>
      <span className="text-sm">{label}</span>
    </a>
  );
}
