import { lazy, Suspense } from 'react'
import { Routes, Route } from 'react-router-dom'
import MainLayout from './components/layout/MainLayout'
import ErrorBoundary from './components/ErrorBoundary'
import { ToastViewport } from './components/Toast'
import { ThemeProvider } from './theme/ThemeProvider'
import { SkeletonStack } from './components/Skeleton'

// Landing-page routes stay eager so first paint is fast.
import AppsPage from './pages/AppsPage'
import SignInPage from './pages/SignInPage'
import SignUpPage from './pages/SignUpPage'
import Callback from './auth/Callback'
import ProtectedRoute from './auth/ProtectedRoute'

// All other routes are lazy-loaded so their JS is only downloaded when
// the user actually navigates to them.
// AppDetailPage is lazy because it is never a first-paint route — it is
// always reached by clicking a row in AppsPage (PR #50, W11 B3.1).
const AppDetailPage = lazy(() => import('./pages/AppDetailPage'))
const NewAppWizard = lazy(() => import('./pages/NewAppWizard'))
const PipelinesPage = lazy(() => import('./pages/PipelinesPage'))
const PipelineEditorPage = lazy(() => import('./pages/PipelineEditorPage'))
const RunPage = lazy(() => import('./pages/RunPage'))
const DockerPage = lazy(() => import('./pages/DockerPage'))
const ComposePage = lazy(() => import('./pages/ComposePage'))
const KubernetesPage = lazy(() => import('./pages/KubernetesPage'))
const EnvironmentsPage = lazy(() => import('./pages/EnvironmentsPage'))
const HostsPage = lazy(() => import('./pages/HostsPage'))
const SettingsPage = lazy(() => import('./pages/SettingsPage'))
const RegistryPage = lazy(() => import('./pages/RegistryPage'))

/**
 * Full-viewport skeleton shown while a lazy route chunk is being fetched.
 * Keeps the shell (sidebar, topbar) visible while the page content loads.
 */
function RouteLoadingFallback() {
  return (
    <div style={{ padding: '2rem' }}>
      <SkeletonStack rows={6} height={24} gap={16} />
    </div>
  )
}

export default function App() {
  return (
    <ThemeProvider>
      <ErrorBoundary>
        <ToastViewport />
        <Routes>
          <Route path="/signin" element={<SignInPage />} />
          <Route path="/signup" element={<SignUpPage />} />
          <Route path="/callback" element={<Callback />} />
          <Route
            path="/*"
            element={
              <ProtectedRoute>
                <Suspense fallback={<RouteLoadingFallback />}>
                  <Routes>
                    <Route path="/pipelines/:id/edit" element={<MainLayout fullBleed><PipelineEditorPage /></MainLayout>} />
                    <Route path="/pipelines/:id/runs/:runId" element={<MainLayout fullBleed><RunPage /></MainLayout>} />
                    <Route path="/apps/new" element={<MainLayout><NewAppWizard /></MainLayout>} />
                    <Route path="/" element={<MainLayout><AppsPage /></MainLayout>} />
                    <Route path="/apps" element={<MainLayout><AppsPage /></MainLayout>} />
                    <Route path="/apps/:id" element={<MainLayout><AppDetailPage /></MainLayout>} />
                    <Route path="/pipelines" element={<MainLayout><PipelinesPage /></MainLayout>} />
                    <Route path="/docker" element={<MainLayout><DockerPage /></MainLayout>} />
                    <Route path="/docker/compose" element={<MainLayout fullBleed><ComposePage /></MainLayout>} />
                    <Route path="/kubernetes" element={<MainLayout><KubernetesPage /></MainLayout>} />
                    <Route path="/environments" element={<MainLayout><EnvironmentsPage /></MainLayout>} />
                    <Route path="/hosts" element={<MainLayout><HostsPage /></MainLayout>} />
                    <Route path="/settings" element={<MainLayout><SettingsPage /></MainLayout>} />
                    <Route path="/registry" element={<MainLayout><RegistryPage /></MainLayout>} />
                  </Routes>
                </Suspense>
              </ProtectedRoute>
            }
          />
        </Routes>
      </ErrorBoundary>
    </ThemeProvider>
  )
}
