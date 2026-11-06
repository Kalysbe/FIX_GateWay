import type { SVGProps } from "react";
import {
  Activity,
  ArrowLeftRight,
  ArrowUp,
  BookOpen,
  Bell,
  Cable,
  ChartCandlestick,
  ChartLine,
  Check,
  ChevronDown,
  ChevronRight,
  ChevronsLeft,
  ChevronsRight,
  Columns3,
  Download,
  Ellipsis,
  Eye,
  EyeOff,
  FileText,
  Inbox,
  Info,
  KeyRound,
  Landmark,
  Layers,
  LayoutDashboard,
  ListOrdered,
  Lock,
  LogOut,
  Menu,
  Monitor,
  Moon,
  PanelLeft,
  Pencil,
  Plus,
  RadioTower,
  RefreshCw,
  Rows3,
  ScrollText,
  Search,
  Settings,
  ShieldCheck,
  ShoppingCart,
  Star,
  Sun,
  Users,
  Wallet,
  X,
  type LucideIcon,
} from "lucide-react";

export type IconProps = SVGProps<SVGSVGElement> & { size?: number };

// One stroke weight for every icon in the console.
function lucide(Component: LucideIcon, defaultSize = 18) {
  return function Icon({ size = defaultSize, ...props }: IconProps) {
    return <Component size={size} strokeWidth={1.75} aria-hidden {...props} />;
  };
}

function Logo({ size = 18, ...props }: IconProps) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden {...props}>
      <path d="M4 7h16M4 12h10M4 17h14" />
      <circle cx="18" cy="12" r="2" fill="currentColor" stroke="none" />
    </svg>
  );
}

export const Icons = {
  search: lucide(Search),
  sort: lucide(ArrowUp),
  columns: lucide(Columns3),
  density: lucide(Rows3),
  download: lucide(Download),
  logo: Logo,
  dashboard: lucide(LayoutDashboard),
  brokers: lucide(Landmark),
  tokens: lucide(KeyRound),
  orders: lucide(ListOrdered),
  trades: lucide(ArrowLeftRight),
  quotes: lucide(ChartCandlestick),
  accounts: lucide(Wallet),
  instruments: lucide(Layers),
  adapters: lucide(Cable),
  inbox: lucide(Inbox),
  stream: lucide(RadioTower),
  monitoring: lucide(Activity),
  users: lucide(Users),
  roles: lucide(ShieldCheck),
  audit: lucide(ScrollText),
  chevron: lucide(ChevronRight),
  chevronDown: lucide(ChevronDown),
  chevronsLeft: lucide(ChevronsLeft),
  chevronsRight: lucide(ChevronsRight),
  bell: lucide(Bell),
  settings: lucide(Settings),
  docs: lucide(FileText),
  sun: lucide(Sun),
  moon: lucide(Moon),
  monitor: lucide(Monitor),
  logout: lucide(LogOut),
  star: lucide(Star),
  plus: lucide(Plus),
  more: lucide(Ellipsis),
  check: lucide(Check),
  x: lucide(X),
  info: lucide(Info),
  edit: lucide(Pencil),
  lock: lucide(Lock, 14),
  eye: lucide(Eye, 14),
  eyeOff: lucide(EyeOff, 14),
  cart: lucide(ShoppingCart, 14),
  refresh: lucide(RefreshCw, 14),
  book: lucide(BookOpen, 14),
  chart: lucide(ChartLine, 14),
  menu: lucide(Menu),
  panel: lucide(PanelLeft),
};
