import type { ReactNode, SVGProps } from "react";

type IconProps = SVGProps<SVGSVGElement> & { size?: number };

function Icon({ size = 18, children, ...props }: IconProps & { children: ReactNode }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.75"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden
      {...props}
    >
      {children}
    </svg>
  );
}

export const Icons = {
  search: (p: IconProps) => <Icon {...p}><circle cx="10.5" cy="10.5" r="6.5" /><path d="m16 16 4.5 4.5" /></Icon>,
  sort: (p: IconProps) => <Icon {...p}><path d="M12 4v16m-5-11 5-5 5 5" /></Icon>,
  columns: (p: IconProps) => <Icon {...p}><rect x="3" y="4" width="18" height="16" rx="2" /><path d="M9 4v16M15 4v16" /></Icon>,
  density: (p: IconProps) => <Icon {...p}><path d="M4 5h16M4 10h16M4 15h16M4 20h16" /></Icon>,
  download: (p: IconProps) => <Icon {...p}><path d="M12 3v12m-4-4 4 4 4-4M4 17v4h16v-4" /></Icon>,
  logo: (p: IconProps) => (
    <Icon {...p}>
      <path d="M4 7h16M4 12h10M4 17h14" />
      <circle cx="18" cy="12" r="2" fill="currentColor" stroke="none" />
    </Icon>
  ),
  dashboard: (p: IconProps) => (
    <Icon {...p}>
      <rect x="3" y="3" width="7" height="7" rx="1.5" />
      <rect x="14" y="3" width="7" height="7" rx="1.5" />
      <rect x="3" y="14" width="7" height="7" rx="1.5" />
      <rect x="14" y="14" width="7" height="7" rx="1.5" />
    </Icon>
  ),
  brokers: (p: IconProps) => (
    <Icon {...p}>
      <path d="M3 21h18" />
      <path d="M5 21V7l7-4 7 4v14" />
      <path d="M9 21v-6h6v6" />
    </Icon>
  ),
  tokens: (p: IconProps) => (
    <Icon {...p}>
      <circle cx="8" cy="12" r="4" />
      <path d="M12 12h9M18 9v6" />
    </Icon>
  ),
  orders: (p: IconProps) => (
    <Icon {...p}>
      <path d="M8 6h13M8 12h13M8 18h13" />
      <path d="M3 6h.01M3 12h.01M3 18h.01" />
    </Icon>
  ),
  trades: (p: IconProps) => (
    <Icon {...p}>
      <path d="M12 3v18" />
      <path d="M7 8h10" />
      <path d="M7 16h10" />
      <circle cx="12" cy="8" r="2.5" />
      <circle cx="12" cy="16" r="2.5" />
    </Icon>
  ),
  quotes: (p: IconProps) => (
    <Icon {...p}>
      <path d="M4 19V5" />
      <path d="M4 19h16" />
      <path d="M7 14l3-4 2 2 4-5" />
      <path d="M16 8h3v3" />
    </Icon>
  ),
  accounts: (p: IconProps) => (
    <Icon {...p}>
      <rect x="3" y="5" width="18" height="14" rx="2" />
      <path d="M3 10h18" />
      <path d="M7 15h4" />
    </Icon>
  ),
  instruments: (p: IconProps) => (
    <Icon {...p}>
      <path d="M4 19V5" />
      <path d="M4 19h16" />
      <path d="M8 15l3-4 3 2 4-6" />
    </Icon>
  ),
  adapters: (p: IconProps) => (
    <Icon {...p}>
      <rect x="2" y="7" width="8" height="10" rx="1.5" />
      <rect x="14" y="7" width="8" height="10" rx="1.5" />
      <path d="M10 12h4" />
    </Icon>
  ),
  monitoring: (p: IconProps) => (
    <Icon {...p}>
      <path d="M3 12h3l3-7 4 14 3-7h5" />
    </Icon>
  ),
  users: (p: IconProps) => (
    <Icon {...p}>
      <circle cx="9" cy="8" r="3.5" />
      <path d="M2.5 19c1.5-3.5 4-5 6.5-5s5 1.5 6.5 5" />
      <circle cx="17" cy="9" r="2.5" />
      <path d="M16 14c2 .3 3.5 1.4 4.5 3.5" />
    </Icon>
  ),
  roles: (p: IconProps) => (
    <Icon {...p}>
      <path d="M12 3l7 4v5c0 4.5-3 7.5-7 9-4-1.5-7-4.5-7-9V7l7-4z" />
      <path d="M9.5 12l1.8 1.8L15 10" />
    </Icon>
  ),
  audit: (p: IconProps) => (
    <Icon {...p}>
      <path d="M8 4h8a2 2 0 012 2v14l-6-3-6 3V6a2 2 0 012-2z" />
      <path d="M10 9h4M10 13h4" />
    </Icon>
  ),
  chevron: (p: IconProps) => (
    <Icon {...p}>
      <path d="M9 6l6 6-6 6" />
    </Icon>
  ),
  bell: (p: IconProps) => (
    <Icon {...p}>
      <path d="M6 16V10a6 6 0 0112 0v6" />
      <path d="M5 16h14" />
      <path d="M10 19a2 2 0 004 0" />
    </Icon>
  ),
  settings: (p: IconProps) => (
    <Icon {...p}>
      <circle cx="12" cy="12" r="3" />
      <path d="M19.4 15a1.7 1.7 0 00.3 1.8l.1.1a2 2 0 11-2.8 2.8l-.1-.1a1.7 1.7 0 00-1.8-.3 1.7 1.7 0 00-1 1.5V21a2 2 0 11-4 0v-.1a1.7 1.7 0 00-1-1.5 1.7 1.7 0 00-1.8.3l-.1.1a2 2 0 11-2.8-2.8l.1-.1a1.7 1.7 0 00.3-1.8 1.7 1.7 0 00-1.5-1H3a2 2 0 110-4h.1a1.7 1.7 0 001.5-1 1.7 1.7 0 00-.3-1.8l-.1-.1a2 2 0 112.8-2.8l.1.1a1.7 1.7 0 001.8.3H9a1.7 1.7 0 001-1.5V3a2 2 0 114 0v.1a1.7 1.7 0 001 1.5 1.7 1.7 0 001.8-.3l.1-.1a2 2 0 112.8 2.8l-.1.1a1.7 1.7 0 00-.3 1.8V9a1.7 1.7 0 001.5 1H21a2 2 0 110 4h-.1a1.7 1.7 0 00-1.5 1z" />
    </Icon>
  ),
  docs: (p: IconProps) => (
    <Icon {...p}>
      <path d="M8 3h6l4 4v14H8a2 2 0 01-2-2V5a2 2 0 012-2z" />
      <path d="M14 3v4h4M10 12h6M10 16h6" />
    </Icon>
  ),
  sun: (p: IconProps) => (
    <Icon {...p}>
      <circle cx="12" cy="12" r="4" />
      <path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4" />
    </Icon>
  ),
  logout: (p: IconProps) => (
    <Icon {...p}>
      <path d="M9 21H5a2 2 0 01-2-2V5a2 2 0 012-2h4" />
      <path d="M16 17l5-5-5-5" />
      <path d="M21 12H9" />
    </Icon>
  ),
  star: (p: IconProps) => (
    <Icon {...p}>
      <path d="M12 3l2.6 5.3 5.9.9-4.2 4.1 1 5.8L12 16.9 6.7 19.1l1-5.8L3.5 9.2l5.9-.9L12 3z" />
    </Icon>
  ),
  plus: (p: IconProps) => (
    <Icon {...p}>
      <path d="M12 5v14M5 12h14" />
    </Icon>
  ),
  more: (p: IconProps) => (
    <Icon {...p}>
      <circle cx="12" cy="5" r="1.5" fill="currentColor" stroke="none" />
      <circle cx="12" cy="12" r="1.5" fill="currentColor" stroke="none" />
      <circle cx="12" cy="19" r="1.5" fill="currentColor" stroke="none" />
    </Icon>
  ),
  check: (p: IconProps) => (
    <Icon {...p}>
      <path d="M5 12l5 5L20 7" />
    </Icon>
  ),
  x: (p: IconProps) => (
    <Icon {...p}>
      <path d="M6 6l12 12M18 6L6 18" />
    </Icon>
  ),
  info: (p: IconProps) => (
    <Icon {...p}>
      <circle cx="12" cy="12" r="9" />
      <path d="M12 8h.01M11 12h1v4h1" />
    </Icon>
  ),
  edit: (p: IconProps) => (
    <Icon {...p}>
      <path d="M12 20h9" />
      <path d="M16.5 3.5a2.1 2.1 0 013 3L7 19l-4 1 1-4L16.5 3.5z" />
    </Icon>
  ),
  lock: (p: IconProps) => (
    <Icon size={14} {...p}>
      <rect x="5" y="11" width="14" height="10" rx="2" />
      <path d="M8 11V8a4 4 0 018 0v3" />
    </Icon>
  ),
  eye: (p: IconProps) => (
    <Icon size={14} {...p}>
      <path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12z" />
      <circle cx="12" cy="12" r="3" />
    </Icon>
  ),
  cart: (p: IconProps) => (
    <Icon size={14} {...p}>
      <circle cx="9" cy="20" r="1.5" />
      <circle cx="17" cy="20" r="1.5" />
      <path d="M3 4h2l2.2 11h10.3l2-7H7" />
    </Icon>
  ),
  refresh: (p: IconProps) => (
    <Icon size={14} {...p}>
      <path d="M21 12a9 9 0 11-2.6-6.3" />
      <path d="M21 3v6h-6" />
    </Icon>
  ),
  book: (p: IconProps) => (
    <Icon size={14} {...p}>
      <path d="M4 5a2 2 0 012-2h12v16H6a2 2 0 00-2 2V5z" />
      <path d="M6 3v16" />
    </Icon>
  ),
  chart: (p: IconProps) => (
    <Icon size={14} {...p}>
      <path d="M4 19V5M4 19h16" />
      <path d="M8 15l3-4 3 2 4-6" />
    </Icon>
  ),
  menu: (p: IconProps) => (
    <Icon {...p}>
      <path d="M4 7h16M4 12h16M4 17h16" />
    </Icon>
  ),
};
