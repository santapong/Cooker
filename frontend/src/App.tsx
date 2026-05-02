import { Routes, Route } from 'react-router-dom'
import MainLayout from './components/layout/MainLayout'
import AppsPage from './pages/AppsPage'
import AppDetailPage from './pages/AppDetailPage'
import PipelinesPage from './pages/PipelinesPage'
import PipelineEditorPage from './pages/PipelineEditorPage'
import DockerPage from './pages/DockerPage'
import ComposePage from './pages/ComposePage'
import KubernetesPage from './pages/KubernetesPage'
import EnvironmentsPage from './pages/EnvironmentsPage'
import Callback from './auth/Callback'
import ProtectedRoute from './auth/ProtectedRoute'

export default function App() {
  return (
    <Routes>
      <Route path="/callback" element={<Callback />} />
      <Route
        path="/*"
        element={
          <ProtectedRoute>
            <MainLayout>
              <Routes>
                <Route path="/" element={<AppsPage />} />
                <Route path="/apps" element={<AppsPage />} />
                <Route path="/apps/:id" element={<AppDetailPage />} />
                <Route path="/pipelines" element={<PipelinesPage />} />
                <Route path="/pipelines/:id/edit" element={<PipelineEditorPage />} />
                <Route path="/docker" element={<DockerPage />} />
                <Route path="/docker/compose" element={<ComposePage />} />
                <Route path="/kubernetes" element={<KubernetesPage />} />
                <Route path="/environments" element={<EnvironmentsPage />} />
              </Routes>
            </MainLayout>
          </ProtectedRoute>
        }
      />
    </Routes>
  )
}
