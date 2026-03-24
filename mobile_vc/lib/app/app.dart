import 'package:flutter/material.dart';

import '../features/session/session_controller.dart';
import '../features/session/session_home_page.dart';
import 'theme.dart';

class MobileVcApp extends StatefulWidget {
  const MobileVcApp({super.key});

  @override
  State<MobileVcApp> createState() => _MobileVcAppState();
}

class _MobileVcAppState extends State<MobileVcApp> {
  late final SessionController _controller;

  @override
  void initState() {
    super.initState();
    _controller = SessionController();
    _controller.initialize();
  }

  @override
  void dispose() {
    _controller.disposeController();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return AnimatedBuilder(
      animation: _controller,
      builder: (context, _) {
        return MaterialApp(
          title: 'MobileVC',
          debugShowCheckedModeBanner: false,
          theme: AppTheme.light(),
          home: SessionHomePage(controller: _controller),
        );
      },
    );
  }
}
