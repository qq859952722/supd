// 图标映射组件 — 将 icon 字段名映射为 lucide-react 图标组件
import {
  Server, Database, Globe, Cloud, HardDrive, Box,
  Container, Cpu, Network, Shield, Zap, Code,
  Terminal, FileText, Settings, Activity, Monitor,
  Download, Trash2, Mail, MessageSquare, Calendar,
  Lock, Key, Bug, Bell, Play, Video, Music,
  Image, Camera, Folder, Upload, Wifi, Router, Link,
  Rocket, Wrench, Book, Bot, Brain, Sparkles, Package,
  Layers, Globe2, GitBranch, Hammer, Headphones, Tv,
  Gamepad, Smartphone, Printer, Flame, Wind, Sun, Moon,
  Star, Heart, Flag, Bookmark, Anchor, Compass,
  type LucideIcon,
} from 'lucide-react'

const iconMap: Record<string, LucideIcon> = {
  // 基础设施
  server: Server,
  database: Database,
  db: Database,
  mysql: Database,
  postgres: Database,
  redis: Database,
  cloud: Cloud,
  storage: HardDrive,
  disk: HardDrive,
  box: Box,
  container: Container,
  docker: Container,
  cpu: Cpu,
  network: Network,
  router: Router,
  wifi: Wifi,

  // Web/网络
  globe: Globe,
  web: Globe,
  http: Globe,
  nginx: Globe,
  globe2: Globe2,
  link: Link,

  // 安全
  shield: Shield,
  security: Shield,
  lock: Lock,
  key: Key,
  auth: Key,

  // 开发工具
  code: Code,
  terminal: Terminal,
  git: GitBranch,
  gitbranch: GitBranch,
  package: Package,
  bug: Bug,
  settings: Settings,
  gear: Settings,
  wrench: Wrench,
  hammer: Hammer,
  layers: Layers,

  // 媒体
  play: Play,
  video: Video,
  music: Music,
  image: Image,
  camera: Camera,
  headphones: Headphones,
  tv: Tv,
  gamepad: Gamepad,

  // 文件
  file: FileText,
  fileText: FileText,
  folder: Folder,
  download: Download,
  backup: Download,
  upload: Upload,
  trash: Trash2,
  cleanup: Trash2,

  // 通信
  mail: Mail,
  email: Mail,
  message: MessageSquare,
  chat: MessageSquare,
  bell: Bell,
  notification: Bell,
  calendar: Calendar,
  cron: Calendar,
  schedule: Calendar,

  // 监控
  activity: Activity,
  monitor: Monitor,
  pulse: Activity,

  // AI/智能
  bot: Bot,
  brain: Brain,
  ai: Brain,
  sparkles: Sparkles,

  // 杂项
  zap: Zap,
  power: Zap,
  rocket: Rocket,
  deploy: Rocket,
  book: Book,
  docs: Book,
  fire: Flame,
  wind: Wind,
  sun: Sun,
  moon: Moon,
  star: Star,
  heart: Heart,
  flag: Flag,
  bookmark: Bookmark,
  anchor: Anchor,
  compass: Compass,
  smartphone: Smartphone,
  printer: Printer,
}

// 所有可选图标列表（用于图标选择器）
// 按类别组织，value 为存储到 service.yaml 的字符串
export const AVAILABLE_ICONS: Array<{ value: string; category: string }> = [
  // 基础设施
  { value: 'box', category: '基础设施' },
  { value: 'server', category: '基础设施' },
  { value: 'database', category: '基础设施' },
  { value: 'cloud', category: '基础设施' },
  { value: 'container', category: '基础设施' },
  { value: 'cpu', category: '基础设施' },
  { value: 'network', category: '基础设施' },
  { value: 'router', category: '基础设施' },
  { value: 'wifi', category: '基础设施' },
  { value: 'storage', category: '基础设施' },
  { value: 'layers', category: '基础设施' },

  // Web/网络
  { value: 'globe', category: 'Web/网络' },
  { value: 'globe2', category: 'Web/网络' },
  { value: 'link', category: 'Web/网络' },

  // 安全
  { value: 'shield', category: '安全' },
  { value: 'lock', category: '安全' },
  { value: 'key', category: '安全' },

  // 开发工具
  { value: 'code', category: '开发工具' },
  { value: 'terminal', category: '开发工具' },
  { value: 'git', category: '开发工具' },
  { value: 'package', category: '开发工具' },
  { value: 'bug', category: '开发工具' },
  { value: 'settings', category: '开发工具' },
  { value: 'wrench', category: '开发工具' },
  { value: 'hammer', category: '开发工具' },

  // 媒体
  { value: 'play', category: '媒体' },
  { value: 'video', category: '媒体' },
  { value: 'music', category: '媒体' },
  { value: 'image', category: '媒体' },
  { value: 'camera', category: '媒体' },
  { value: 'headphones', category: '媒体' },
  { value: 'tv', category: '媒体' },
  { value: 'gamepad', category: '媒体' },

  // 文件
  { value: 'file', category: '文件' },
  { value: 'folder', category: '文件' },
  { value: 'download', category: '文件' },
  { value: 'upload', category: '文件' },
  { value: 'trash', category: '文件' },

  // 通信
  { value: 'mail', category: '通信' },
  { value: 'message', category: '通信' },
  { value: 'bell', category: '通信' },
  { value: 'calendar', category: '通信' },

  // 监控
  { value: 'activity', category: '监控' },
  { value: 'monitor', category: '监控' },

  // AI/智能
  { value: 'bot', category: 'AI/智能' },
  { value: 'brain', category: 'AI/智能' },
  { value: 'sparkles', category: 'AI/智能' },

  // 杂项
  { value: 'zap', category: '杂项' },
  { value: 'rocket', category: '杂项' },
  { value: 'book', category: '杂项' },
  { value: 'fire', category: '杂项' },
  { value: 'wind', category: '杂项' },
  { value: 'sun', category: '杂项' },
  { value: 'moon', category: '杂项' },
  { value: 'star', category: '杂项' },
  { value: 'heart', category: '杂项' },
  { value: 'flag', category: '杂项' },
  { value: 'bookmark', category: '杂项' },
  { value: 'anchor', category: '杂项' },
  { value: 'compass', category: '杂项' },
  { value: 'smartphone', category: '杂项' },
  { value: 'printer', category: '杂项' },
]

interface IconRendererProps {
  name?: string
  className?: string
  fallback?: LucideIcon
}

export function IconRenderer({ name, className, fallback: Fallback = Server }: IconRendererProps) {
  if (!name) {
    return <Fallback className={className} />
  }
  const Icon = iconMap[name.toLowerCase()] ?? iconMap[name] ?? Fallback
  return <Icon className={className} />
}
