// REQ-U-002: 路由配置（7个Tab + 详情页）
// react-router v7 路由定义

import { createBrowserRouter } from 'react-router'
import { App } from '@/App'
import { Dashboard } from '@/pages/Dashboard'
import { Services } from '@/pages/Services'
import { ServiceDetail } from '@/pages/ServiceDetail'
import ExtensionsPage from '@/pages/Extensions'
import ExtensionDetailPage from '@/pages/ExtensionDetail'
import CronTasksPage from '@/pages/CronTasks'
import EventsPage from '@/pages/Events'
import FilesPage from '@/pages/Files'
import SettingsPage from '@/pages/Settings'

export const router = createBrowserRouter([
  {
    path: '/',
    element: <App />,
    children: [
      {
        index: true,
        element: <Dashboard />,
      },
      {
        path: 'services',
        element: <Services />,
      },
      {
        path: 'services/:name',
        element: <ServiceDetail />,
      },
      {
        path: 'extensions',
        element: <ExtensionsPage />,
      },
      {
        path: 'extensions/:name',
        element: <ExtensionDetailPage />,
      },
      {
        path: 'cron',
        element: <CronTasksPage />,
      },
      {
        path: 'events',
        element: <EventsPage />,
      },
      {
        path: 'files',
        element: <FilesPage />,
      },
      {
        path: 'settings',
        element: <SettingsPage />,
      },
    ],
  },
])
