

import { BrowserRouter as Router, Routes, Route, Navigate, Link } from 'react-router-dom';
import { GoogleOAuthProvider, GoogleLogin } from '@react-oauth/google';
import ActionList from './ActionList';
import Settings from './Settings';
import './App.css';
import { useEffect, useState } from 'react';
import { fetchAuthStatus, login, logout } from './api';

function App() {
  const [isAdmin, setIsAdmin] = useState(false);
  const [authRequired, setAuthRequired] = useState(false);
  const [loggedIn, setLoggedIn] = useState(false);
  const [clientID, setClientID] = useState("");
  const [loading, setLoading] = useState(true);

  const checkStatus = () => {
    fetchAuthStatus()
      .then(status => {
        setIsAdmin(status.isAdmin);
        setAuthRequired(status.authRequired);
        setLoggedIn(status.loggedIn);
        setClientID(status.clientID);
        setLoading(false);
      })
      .catch(err => {
        console.error(err);
        setLoading(false);
      });
  };

  useEffect(() => {
    checkStatus();
  }, []);

  const handleLoginSuccess = async (credentialResponse: any) => {
    try {
      if (credentialResponse.credential) {
        await login(credentialResponse.credential);
        checkStatus();
      }
    } catch (err) {
      console.error("Login failed", err);
    }
  };

  const handleLogout = async () => {
    try {
        await logout();
        checkStatus();
    } catch (err) {
        console.error("Logout failed", err);
    }
  };

  if (loading) {
    return <div>Loading...</div>;
  }

  // If auth is required and we are not logged in, show login screen
  if (authRequired && !loggedIn) {
    if (!clientID) {
        return <div>Configuration Error: Missing Client ID</div>;
    }
    return (
      <GoogleOAuthProvider clientId={clientID}>
        <div className="login-container">
          <h1>AutoEnergy Login</h1>
          <GoogleLogin
            onSuccess={handleLoginSuccess}
            onError={() => {
              console.log('Login Failed');
            }}
          />
        </div>
      </GoogleOAuthProvider>
    );
  }

  return (
    <Router>
      <div className="app-container">
        <header className="main-header">
            <h1>AutoEnergy</h1>
            <nav>
                <Link to="/">Home</Link>
                &nbsp;<Link to="/settings">Settings</Link>
                {loggedIn && <>&nbsp;<button onClick={handleLogout} className="logout-button">Logout</button></>}
            </nav>
        </header>
        <main>
          <Routes>
            <Route path="/" element={<ActionList />} />
            <Route path="/settings" element={<Settings isAdmin={isAdmin} />} />
            <Route path="*" element={<Navigate to="/" replace />} />
          </Routes>
        </main>
      </div>
    </Router>
  );
}

export default App;
