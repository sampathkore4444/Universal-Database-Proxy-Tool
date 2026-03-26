import React, { createContext, useContext, useState, useCallback, ReactNode } from 'react';
import { Snackbar, Alert, AlertColor, Slide, SlideProps } from '@mui/material';

type SlideTransitionProps = SlideProps & {
  children: React.ReactElement<any, any>;
};

function SlideTransition(props: SlideTransitionProps) {
  return <Slide {...props} direction="up" />;
}

interface Notification {
  id: number;
  message: string;
  severity: AlertColor;
}

interface NotificationContextType {
  showNotification: (message: string, severity?: AlertColor) => void;
  showSuccess: (message: string) => void;
  showError: (message: string) => void;
  showWarning: (message: string) => void;
  showInfo: (message: string) => void;
}

const NotificationContext = createContext<NotificationContextType | undefined>(undefined);

export const useNotification = () => {
  const context = useContext(NotificationContext);
  if (!context) throw new Error('useNotification must be used within NotificationProvider');
  return context;
};

interface NotificationProviderProps {
  children: ReactNode;
}

export const NotificationProvider: React.FC<NotificationProviderProps> = ({ children }) => {
  const [notifications, setNotifications] = useState<Notification[]>([]);

  const showNotification = useCallback((message: string, severity: AlertColor = 'info') => {
    const id = Date.now();
    setNotifications(prev => [...prev, { id, message, severity }]);
  }, []);

  const showSuccess = (message: string) => showNotification(message, 'success');
  const showError = (message: string) => showNotification(message, 'error');
  const showWarning = (message: string) => showNotification(message, 'warning');
  const showInfo = (message: string) => showNotification(message, 'info');

  const handleClose = (id: number) => {
    setNotifications(prev => prev.filter(n => n.id !== id));
  };

  return (
    <NotificationContext.Provider value={{ showNotification, showSuccess, showError, showWarning, showInfo }}>
      {children}
      {notifications.map((notification, index) => (
        <Snackbar
          key={notification.id}
          open={true}
          autoHideDuration={5000}
          onClose={() => handleClose(notification.id)}
          anchorOrigin={{ vertical: 'bottom', horizontal: 'right' }}
          TransitionComponent={SlideTransition}
          sx={{ bottom: `${(index * 60) + 24}px !important` }}
        >
          <Alert
            onClose={() => handleClose(notification.id)}
            severity={notification.severity}
            variant="filled"
            sx={{ minWidth: 300, boxShadow: '0 4px 20px rgba(0,0,0,0.15)' }}
          >
            {notification.message}
          </Alert>
        </Snackbar>
      ))}
    </NotificationContext.Provider>
  );
};
